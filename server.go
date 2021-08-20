package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	server "github.com/yinhm/friendfeed/server"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
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

	filename := fmt.Sprintf("ffdb.%s.log", time.Now().Format("20060102"))
	logf, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer logf.Close()

	log.SetOutput(io.MultiWriter(logf, os.Stdout))
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	util.RedirectStderr(logf)

	if cfg.Debug {
		server.SetLogLevel(logrus.DebugLevel)
		server.SetLogFile(logf)
		log.Printf("verbose log mode enabled\n")
	}

	lis, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("Rpc server running at %s", cfg.Address)

	rpcServer := grpc.NewServer()
	apiServer := server.NewApiServer(cfg.DBPath, cfg)

	// index service
	search.InitIndexService(filepath.Join(cfg.DBPath, "index"))

	go apiServer.RefetchJobTicker()
	go apiServer.IndexJobTicker()
	go waitShutdown(rpcServer, apiServer)

	pb.RegisterApiServer(rpcServer, apiServer)
	rpcServer.Serve(lis)
}
