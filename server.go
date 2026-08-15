package main

import (
	"errors"
	"flag"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	server "github.com/yinhm/friendfeed/server"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
)

const (
	MaxReceiveMessageSize = 1024 * 1024 * 64
)

var cfgFile string
var debug bool

func init() {
	flag.StringVar(&cfgFile, "c", "/srv/ffdb/config.json", "config filepath")
	flag.BoolVar(&debug, "d", false, "debug mode")
}

func waitShutdown(rpcSrv *grpc.Server, apiSrv *server.ApiServer, health *healthCheck) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Block until a signal is received or we got an error
	signal := <-sigCh

	log.Printf("Signal %s received, shutdown server...", signal)
	// Flip health to NOT_SERVING before draining so new probes fail
	// while in-flight requests finish.
	health.shutdown()
	apiSrv.StopTaskClaims()
	rpcSrv.GracefulStop()
	log.Println("rpc server stopped.")
	apiSrv.Shutdown()
	log.Println("api server stopped.")
	if err := search.CloseIndexService(); err != nil {
		log.Printf("failed to close search index: %v", err)
	}
	log.Println("index server closed.")
}

func main() {
	flag.Parse()

	cfg, err := util.NewConfigFromJSON(cfgFile)
	if err != nil {
		log.Fatal(err)
	}
	if debug {
		cfg.Debug = debug
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	slogLevel := slog.LevelInfo
	if cfg.Debug {
		slogLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})))
	if cfg.Debug {
		slog.Debug("verbose log mode enabled")
	}

	lis, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("Rpc server running at %s", cfg.Address)

	rpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(MaxReceiveMessageSize))
	health := newHealthCheck(rpcServer)
	apiServer, err := server.NewApiServer(cfg.DBPath, cfg)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	health.markServing(healthServiceStorage)

	// index service: main owns the exit policy on initialization
	// failure; library code must not call log.Fatal.
	if err := search.InitIndexServiceE(filepath.Join(cfg.DBPath, "index")); err != nil {
		log.Fatalf("failed to initialize search index: %v", err)
	}
	health.markServing(healthServiceSearch)

	apiServer.StartBackgroundJobs()
	shutdownDone := make(chan struct{})
	go func() {
		waitShutdown(rpcServer, apiServer, health)
		close(shutdownDone)
	}()

	pb.RegisterApiServer(rpcServer, apiServer)
	health.markReady()
	// Serve returns nil once GracefulStop completes; main must not
	// return before waitShutdown finished closing the index and db,
	// otherwise process exit kills the cleanup mid-flight.
	if err := rpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		log.Fatalf("RPC server failed: %v", err)
	}
	<-shutdownDone
}
