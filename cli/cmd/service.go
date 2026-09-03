package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

var (
	serviceAddFeed  string
	serviceAddKind  string
	serviceAddApply bool
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "manage built-in Feed services",
}

var serviceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "bind a built-in service to a Feed",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if serviceAddKind != model.BingWallpaperServiceKind {
			return fmt.Errorf("unsupported built-in service kind %q", serviceAddKind)
		}
		if serviceAddFeed == "" {
			return errors.New("--feed is required")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		inspected, err := apiClient.InspectFeed(ctx, &pb.InspectFeedRequest{Feed: serviceAddFeed})
		if err != nil {
			return fmt.Errorf("inspect target Feed %q: %w", serviceAddFeed, err)
		}
		if inspected.Profile == nil || inspected.Profile.Uuid == "" || inspected.Profile.Deleted {
			return errors.New("target Feed is missing or deleted")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Feed: %s (%s)\nkind: %s\n", inspected.Profile.Id, inspected.Profile.Uuid, serviceAddKind)
		if !serviceAddApply {
			fmt.Fprintln(cmd.OutOrStdout(), "dry-run only; rerun with --apply to create the binding")
			return nil
		}
		binding, err := apiClient.AddFeedService(ctx, &pb.AddFeedServiceRequest{
			TargetFeedUuid: inspected.Profile.Uuid,
			Kind:           serviceAddKind,
		})
		if err != nil {
			return fmt.Errorf("add %s service to Feed %q: %w", serviceAddKind, serviceAddFeed, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "bound service %s\n", binding.ServiceUuid)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(serviceAddCmd)
	serviceAddCmd.Flags().StringVar(&serviceAddFeed, "feed", "", "target Feed ID or UUID")
	serviceAddCmd.Flags().StringVar(&serviceAddKind, "kind", "", "built-in service kind")
	serviceAddCmd.Flags().BoolVar(&serviceAddApply, "apply", false, "create the binding")
}
