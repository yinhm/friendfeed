package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var runTaskName string

var rumCmd = &cobra.Command{
	Use:   "run",
	Short: "run a specific job",
	Long: `执行特定任务:

    // Mark deletion
    // client run --t="MarkDelete" foobar
    `,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("run task: %s\n", runTaskName)
		if runTaskName == "" {
			return
		}

		tcCfg := &TwitterConfig{
			ApiKey:    viper.GetString("twitter_api_key"),
			ApiSecret: viper.GetString("twitter_api_secret"),
		}

		conn, err := grpc.Dial(config.address, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("Connection error: %v", err)
		}
		defer conn.Close()

		agent := NewFeedAgent(conn, tcCfg)

		cmdReq := &pb.CommandRequest{
			Command: runTaskName,
		}
		if len(args) > 0 && args[0] != "" {
			cmdReq.Arg1 = args[0]
		}
		agent.client.Command(context.Background(), cmdReq)
	},
}

func init() {
	rootCmd.AddCommand(rumCmd)
	rumCmd.Flags().StringVar(&runTaskName, "t", "", "task")
}
