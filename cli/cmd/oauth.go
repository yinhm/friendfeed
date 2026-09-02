package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
)

type oauthIdentityReport struct {
	Provider             string `json:"provider"`
	UserID               string `json:"user_id"`
	Name                 string `json:"name"`
	Nickname             string `json:"nickname"`
	FeedID               string `json:"feed_id"`
	FeedUUID             string `json:"feed_uuid"`
	ProfileIdentityCount int    `json:"profile_identity_count"`
}

var oauthUnlinkApply bool

var oauthCmd = &cobra.Command{Use: "oauth", Short: "inspect and unlink OAuth identities"}

func requestOAuthIdentity(cmd *cobra.Command, command, provider, userID string) (oauthIdentityReport, error) {
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	response, err := apiClient.Command(ctx, &pb.CommandRequest{Command: command, Arg1: provider, Arg2: userID})
	if err != nil {
		return oauthIdentityReport{}, err
	}
	var report oauthIdentityReport
	if err := json.Unmarshal([]byte(response.Result), &report); err != nil {
		return report, fmt.Errorf("decode OAuth maintenance response: %w", err)
	}
	return report, nil
}

func printOAuthIdentity(cmd *cobra.Command, report oauthIdentityReport) {
	fmt.Fprintf(cmd.OutOrStdout(), "OAuth: %s:%s name=%q nickname=%q\n", report.Provider, report.UserID, report.Name, report.Nickname)
	fmt.Fprintf(cmd.OutOrStdout(), "Feed: %s (%s); linked identities: %d\n", report.FeedID, report.FeedUUID, report.ProfileIdentityCount)
}

var oauthInspectCmd = &cobra.Command{
	Use: "inspect <provider> <user-id>", Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := requestOAuthIdentity(cmd, "OAuthInspect", args[0], args[1])
		if err == nil {
			printOAuthIdentity(cmd, report)
		}
		return err
	},
}

var oauthUnlinkCmd = &cobra.Command{
	Use: "unlink <provider> <user-id>", Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := requestOAuthIdentity(cmd, "OAuthInspect", args[0], args[1])
		if err != nil {
			return err
		}
		printOAuthIdentity(cmd, report)
		if !oauthUnlinkApply {
			fmt.Fprintln(cmd.OutOrStdout(), "dry-run only; rerun with --apply to unlink")
			return nil
		}
		confirmation := "UNLINK " + report.Provider + ":" + report.UserID
		fmt.Fprintf(cmd.OutOrStdout(), "Type %q to apply: ", confirmation)
		line, readErr := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, os.ErrClosed) && line == "" {
			return readErr
		}
		if strings.TrimSpace(line) != confirmation {
			return errors.New("confirmation did not match; no changes applied")
		}
		result, err := requestOAuthIdentity(cmd, "OAuthUnlink", report.Provider, report.UserID)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "unlinked")
		printOAuthIdentity(cmd, result)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(oauthCmd)
	oauthCmd.AddCommand(oauthInspectCmd, oauthUnlinkCmd)
	oauthUnlinkCmd.Flags().BoolVar(&oauthUnlinkApply, "apply", false, "unlink after typed confirmation")
}
