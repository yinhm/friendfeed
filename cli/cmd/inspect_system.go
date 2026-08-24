package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
)

var systemInspectJSON bool

type systemInspectReport struct {
	CollectedAt string `json:"collected_at"`
	Tasks       struct {
		Ready            int64 `json:"ready"`
		Inflight         int64 `json:"inflight"`
		Dead             int64 `json:"dead"`
		OldestReadyAgeMS int64 `json:"oldest_ready_age_ms"`
		Truncated        bool  `json:"truncated"`
	} `json:"tasks"`
	Services struct {
		Active    int64 `json:"active"`
		Degraded  int64 `json:"degraded"`
		Dead      int64 `json:"dead"`
		Due       int64 `json:"due"`
		Scanned   int64 `json:"scanned"`
		Truncated bool  `json:"truncated"`
	} `json:"services"`
	Timeline struct {
		MaintenanceRunning int `json:"maintenance_running"`
		MaintenanceLimit   int `json:"maintenance_limit"`
		RetryBackoffs      int `json:"retry_backoffs"`
	} `json:"timeline"`
	Notification struct {
		TrimsRunning int64 `json:"trims_running"`
	} `json:"notification"`
	Public struct {
		BumpsSinceTrim int64 `json:"bumps_since_trim"`
		TrimRunning    bool  `json:"trim_running"`
	} `json:"public_timeline"`
}

var inspectSystemCmd = &cobra.Command{
	Use:   "inspect-system",
	Short: "inspect bounded ffdb background-system state",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()
		response, err := agent.client.Command(ctx, &pb.CommandRequest{Command: "SystemInspect"})
		if err != nil {
			return fmt.Errorf("inspect ffdb system: %w", err)
		}
		if systemInspectJSON {
			var value any
			if err := json.Unmarshal([]byte(response.Result), &value); err != nil {
				return fmt.Errorf("decode ffdb system report: %w", err)
			}
			data, err := json.MarshalIndent(value, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		var report systemInspectReport
		if err := json.Unmarshal([]byte(response.Result), &report); err != nil {
			return fmt.Errorf("decode ffdb system report: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "collected: %s\n", report.CollectedAt)
		fmt.Fprintf(cmd.OutOrStdout(), "tasks: ready %d, inflight %d, dead %d, oldest-ready %s, truncated %t\n",
			report.Tasks.Ready, report.Tasks.Inflight, report.Tasks.Dead,
			(time.Duration(report.Tasks.OldestReadyAgeMS) * time.Millisecond).String(), report.Tasks.Truncated)
		fmt.Fprintf(cmd.OutOrStdout(), "services: active %d, degraded %d, dead %d, due %d, scanned %d, truncated %t\n",
			report.Services.Active, report.Services.Degraded, report.Services.Dead,
			report.Services.Due, report.Services.Scanned, report.Services.Truncated)
		fmt.Fprintf(cmd.OutOrStdout(), "timeline: maintenance %d/%d, retry-backoffs %d\n",
			report.Timeline.MaintenanceRunning, report.Timeline.MaintenanceLimit, report.Timeline.RetryBackoffs)
		fmt.Fprintf(cmd.OutOrStdout(), "notification: trims %d\n", report.Notification.TrimsRunning)
		fmt.Fprintf(cmd.OutOrStdout(), "public timeline: bumps %d, trimming %t\n", report.Public.BumpsSinceTrim, report.Public.TrimRunning)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(inspectSystemCmd)
	inspectSystemCmd.Flags().BoolVar(&systemInspectJSON, "json", false, "print the complete report as JSON")
}
