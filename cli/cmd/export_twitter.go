package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
)

var exportTwitterUsersOut string

var exportTwitterUsersCmd = &cobra.Command{
	Use:   "export-twitter-users",
	Short: "export Twitter Feed mappings and fixed sync boundaries as TSV",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if exportTwitterUsersOut == "" {
			return errors.New("--out is required")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()
		response, err := apiClient.Command(ctx, &pb.CommandRequest{Command: "ExportTwitterUsersTSV"})
		if err != nil {
			return fmt.Errorf("export Twitter users: %w", err)
		}
		file, err := os.OpenFile(exportTwitterUsersOut, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("create Twitter users TSV: %w", err)
		}
		if _, err = file.WriteString(response.Result); err == nil {
			err = file.Close()
		} else {
			_ = file.Close()
		}
		if err != nil {
			_ = os.Remove(exportTwitterUsersOut)
			return fmt.Errorf("write Twitter users TSV: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", exportTwitterUsersOut)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(exportTwitterUsersCmd)
	exportTwitterUsersCmd.Flags().StringVar(&exportTwitterUsersOut, "out", "", "TSV output path (required, mode 0600)")
}
