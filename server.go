package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"

	pb "github.com/yinhm/friendfeed/proto"
	server "github.com/yinhm/friendfeed/server"
	"google.golang.org/grpc"
)

var config struct {
	address string
	dbpath  string
	config  string
}

func init() {
	flag.StringVar(&config.address, "addr", ":8901", "RPC Server Url")
	flag.StringVar(&config.dbpath, "db", "/srv/ff/db", "RPC Server Url")
	flag.StringVar(&config.config, "c", "/srv/ff/config.json", "config file")
}

func waitShutdown(rpcSrv *grpc.Server, apiSrv *server.ApiServer) {
	sigCh := make(chan os.Signal)
	signal.Notify(sigCh, os.Interrupt, os.Kill)

	// Block until a signal is received or we got an error
	select {
	case signal := <-sigCh:
		log.Printf("Got signal %s, waiting for shutdown...", signal)
		rpcSrv.Stop()
		apiSrv.Shutdown()
		return
	}
}

func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", config.address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("Rpc server running at %s", config.address)

	rpcServer := grpc.NewServer()
	apiServer := server.NewApiServer(config.dbpath, config.config)

	go apiServer.RefetchJobTicker()
	go apiServer.IndexJobTicker()
	go waitShutdown(rpcServer, apiServer)

	pb.RegisterApiServer(rpcServer, apiServer)
	rpcServer.Serve(lis)
}
