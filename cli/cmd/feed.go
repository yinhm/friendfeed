package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	feedInspectEntries int
	feedInspectJSON    bool
	feedPrivacyFeeds   []string
	feedPrivacyFile    string
	feedPrivacyApply   bool
)

var feedCmd = &cobra.Command{
	Use:   "feed",
	Short: "inspect and manage Feeds",
}

var feedInspectCmd = &cobra.Command{
	Use:   "inspect <id-or-uuid>",
	Short: "inspect bounded Feed metadata and consistency state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		response, err := apiClient.InspectFeed(ctx, &pb.InspectFeedRequest{
			Feed: args[0], EntryLimit: int32(feedInspectEntries),
		})
		if err != nil {
			return fmt.Errorf("inspect Feed %q: %w", args[0], err)
		}
		if feedInspectJSON {
			data, err := (protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}).Marshal(response)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		printFeedInspection(cmd, response)
		return nil
	},
}

var feedPrivacyCmd = &cobra.Command{
	Use:   "privacy",
	Short: "preview or apply user Feed privacy changes",
}

func newFeedPrivacyStateCommand(name string, private bool) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("set selected user Feeds %s", name),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			feeds, err := feedPrivacyIdentifiers(feedPrivacyFeeds, feedPrivacyFile)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			preview, err := apiClient.UpdateFeedState(ctx, &pb.UpdateFeedStateRequest{
				Feeds: feeds, Patch: &pb.FeedStatePatch{Private: &private}, DryRun: true,
			})
			if err != nil {
				return fmt.Errorf("preview Feed privacy change: %w", err)
			}
			changed := printFeedStateChanges(cmd, preview.Changes, "would-change")
			fmt.Fprintf(cmd.OutOrStdout(), "%d feeds checked, %d would change\n", len(preview.Changes), changed)
			if !feedPrivacyApply || changed == 0 {
				if !feedPrivacyApply {
					fmt.Fprintln(cmd.OutOrStdout(), "dry-run only; rerun with --apply to write changes")
				}
				return nil
			}

			confirmation := fmt.Sprintf("%s %d", strings.ToUpper(name), changed)
			fmt.Fprintf(cmd.OutOrStdout(), "Type %q to apply: ", confirmation)
			line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if err != nil && !errors.Is(err, os.ErrClosed) && len(line) == 0 {
				return fmt.Errorf("read confirmation: %w", err)
			}
			if strings.TrimSpace(line) != confirmation {
				return errors.New("confirmation did not match; no changes applied")
			}

			applyCtx, applyCancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer applyCancel()
			applied, err := apiClient.UpdateFeedState(applyCtx, &pb.UpdateFeedStateRequest{
				Feeds: feeds, Patch: &pb.FeedStatePatch{Private: &private}, DryRun: false,
			})
			if err != nil {
				return fmt.Errorf("apply Feed privacy change: %w", err)
			}
			appliedCount := printFeedStateChanges(cmd, applied.Changes, "changed")
			fmt.Fprintf(cmd.OutOrStdout(), "%d feeds checked, %d changed\n", len(applied.Changes), appliedCount)
			return nil
		},
	}
}

func feedPrivacyIdentifiers(flags []string, filename string) ([]string, error) {
	values := append([]string(nil), flags...)
	if filename != "" {
		file, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("open Feed list: %w", err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			values = append(values, line)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read Feed list: %w", err)
		}
	}
	seen := make(map[string]struct{}, len(values))
	feeds := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("Feed identifier cannot be empty")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		feeds = append(feeds, value)
	}
	if len(feeds) == 0 {
		return nil, errors.New("at least one --feed or --file entry is required")
	}
	if len(feeds) > 100 {
		return nil, fmt.Errorf("at most 100 Feed identifiers are allowed, got %d", len(feeds))
	}
	return feeds, nil
}

func printFeedInspection(cmd *cobra.Command, response *pb.InspectFeedResponse) {
	profile := response.Profile
	fmt.Fprintf(cmd.OutOrStdout(), "Feed: %s (%s)\n", profile.Id, profile.Uuid)
	fmt.Fprintf(cmd.OutOrStdout(), "type: %s, private: %t, deleted: %t, super: %t\n", profile.Type, profile.Private, profile.Deleted, profile.IsSuper)
	fmt.Fprintf(cmd.OutOrStdout(), "UserMap consistent: %t\n", response.UserMapConsistent)
	fmt.Fprintf(cmd.OutOrStdout(), "entries: %d, following: %d, followers: %d, pending requests: %d\n",
		response.EntryCount, response.FollowingCount, response.FollowerCount, response.PendingRequestCount)
	fmt.Fprintf(cmd.OutOrStdout(), "services: %d, Group admins: %d, Group members: %d\n",
		response.ServiceCount, response.GroupAdminCount, response.GroupMemberCount)
	fmt.Fprintf(cmd.OutOrStdout(), "timeline state: %t", response.TimelineStateExists)
	if response.TimelineStateExists {
		fmt.Fprintf(cmd.OutOrStdout(), " (last access %s)", time.UnixMilli(response.TimelineLastAccessMs).UTC().Format(time.RFC3339))
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(), "archive: exists=%t dirty=%t; Feed API key: exists=%t active=%t\n",
		response.ArchiveExists, response.ArchiveDirty, response.FeedApiKeyExists, response.FeedApiKeyActive)
	if len(response.Entries) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "recent entries:")
		for _, entry := range response.Entries {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s  author=%s target=%s\n", entry.Uuid,
				time.UnixMilli(entry.CreatedAtMs).UTC().Format(time.RFC3339), entry.ProfileUuid, entry.FeedUuid)
		}
	}
	for _, warning := range response.Warnings {
		fmt.Fprintf(cmd.OutOrStdout(), "warning: %s\n", warning)
	}
}

func printFeedStateChanges(cmd *cobra.Command, changes []*pb.FeedStateChange, changedLabel string) int {
	fmt.Fprintln(cmd.OutOrStdout(), "FEED\tTYPE\tCURRENT\tTARGET\tRESULT")
	changed := 0
	for _, change := range changes {
		result := "unchanged"
		if change.Changed {
			result = changedLabel
			changed++
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", change.Id, change.Type,
			privacyName(change.Before.Private), privacyName(change.After.Private), result)
	}
	return changed
}

func privacyName(private bool) string {
	if private {
		return "private"
	}
	return "public"
}

func init() {
	rootCmd.AddCommand(feedCmd)
	feedCmd.AddCommand(feedInspectCmd, feedPrivacyCmd)
	feedInspectCmd.Flags().IntVar(&feedInspectEntries, "entries", 20, "number of recent Entry identities to include (0-100)")
	feedInspectCmd.Flags().BoolVar(&feedInspectJSON, "json", false, "print the complete response as JSON")
	for _, command := range []*cobra.Command{
		newFeedPrivacyStateCommand("private", true),
		newFeedPrivacyStateCommand("public", false),
	} {
		feedPrivacyCmd.AddCommand(command)
		command.Flags().StringArrayVar(&feedPrivacyFeeds, "feed", nil, "Feed ID or UUID (repeatable)")
		command.Flags().StringVar(&feedPrivacyFile, "file", "", "file containing one Feed ID or UUID per line")
		command.Flags().BoolVar(&feedPrivacyApply, "apply", false, "apply the previewed changes after typed confirmation")
	}
}
