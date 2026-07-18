package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sirupsen/logrus"
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

func waitShutdown(rpcSrv *grpc.Server, apiSrv *server.ApiServer) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Block until a signal is received or we got an error
	signal := <-sigCh

	log.Printf("Signal %s received, shutdown server...", signal)
	apiSrv.Shutdown()
	log.Println("api server stoped.")
	search.Indexer.Close()
	log.Println("index server closed.")
	rpcSrv.Stop()
	log.Println("rpc server stoped.")
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

	if cfg.Debug {
		server.SetLogLevel(logrus.DebugLevel)
		log.Printf("verbose log mode enabled\n")
	}

	lis, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("Rpc server running at %s", cfg.Address)

	rpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(MaxReceiveMessageSize))
	apiServer := server.NewApiServer(cfg.DBPath, cfg)

	// index service
	search.InitIndexService(filepath.Join(cfg.DBPath, "index"))

	go apiServer.RefetchJobTicker()
	go apiServer.IndexJobTicker()
	go waitShutdown(rpcServer, apiServer)

	pb.RegisterApiServer(rpcServer, apiServer)
	rpcServer.Serve(lis)
}
