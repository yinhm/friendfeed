package cmd

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var config struct {
	address string
}

var apiClient pb.ApiClient
var rpcConn *grpc.ClientConn

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "client",
	Short: "ffdb同步数据客户端",
	Long:  `CLI客户端，--help for more information`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		var err error
		rpcConn, err = grpc.Dial(config.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("Connection error: %v", err)
		}
		apiClient = pb.NewApiClient(rpcConn)
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		defer rpcConn.Close()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	rootCmd.PersistentFlags().StringVar(&config.address, "address", "localhost:3000", "RPC Server address")
}
