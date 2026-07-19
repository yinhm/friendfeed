package cmd

import (
	"fmt"

	"context"
	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
)

var runTaskName string

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run a specific job",
	Long: `执行特定任务:

 Example: run MarkDelete task to delete a feed
 cli run --t MarkDelete foobar

List of Jobs
  - ReportJobs
  - ReportRunningJobs
  - PurgeJobs
  - FixTooMuchJobs
  - RedoFailedJob
  - RefetchUserFeed
  - TestJob
  - PurgePrefix
  - MarkDelete
  - SuperAdmin
  - DBMetrics
`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("run task: %s\n", runTaskName)
		if runTaskName == "" {
			return
		}
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
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVar(&runTaskName, "t", "", "task")
}
