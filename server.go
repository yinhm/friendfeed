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
	"time"

	"github.com/sirupsen/logrus"
	pb "github.com/yinhm/friendfeed/proto"
	"github.com/yinhm/friendfeed/search"
	server "github.com/yinhm/friendfeed/server"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
)

var config struct {
	address string
	dbpath  string
	config  string
	debug   bool
}

func init() {
	flag.StringVar(&config.address, "addr", ":8901", "RPC Server Url")
	flag.StringVar(&config.dbpath, "db", "/srv/ffdb/db", "RPC Server Url")
	flag.StringVar(&config.config, "c", "/srv/ffdb/config.json", "config file")
	flag.BoolVar(&config.debug, "d", false, "debug mode")
}

func waitShutdown(rpcSrv *grpc.Server, apiSrv *server.ApiServer) {
	sigCh := make(chan os.Signal)
	signal.Notify(sigCh, os.Interrupt, os.Kill)

	// Block until a signal is received or we got an error
	select {
	case signal := <-sigCh:
		log.Printf("Signal %s received, shutdown server...", signal)
		apiSrv.Shutdown()
		log.Println("api server stoped.")
		search.Indexer.Close()
		log.Println("index server closed.")
		rpcSrv.Stop()
		log.Println("rpc server stoped.")
		return
	}
}

func main() {
	flag.Parse()

	filename := fmt.Sprintf("ffdb.%s.log", time.Now().Format("20060102"))
	logf, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer logf.Close()

	log.SetOutput(io.MultiWriter(logf, os.Stdout))
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	util.RedirectStderr(logf)

	if config.debug {
		server.SetLogLevel(logrus.DebugLevel)
		server.SetLogFile(logf)
		log.Printf("verbose log mode enabled\n")
	}

	lis, err := net.Listen("tcp", config.address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("Rpc server running at %s", config.address)

	rpcServer := grpc.NewServer()
	apiServer := server.NewApiServer(config.dbpath, config.config)

	// index service
	search.InitIndexService(filepath.Join(config.dbpath, "index"))

	go apiServer.RefetchJobTicker()
	go apiServer.IndexJobTicker()
	go waitShutdown(rpcServer, apiServer)

	pb.RegisterApiServer(rpcServer, apiServer)
	rpcServer.Serve(lis)
}
