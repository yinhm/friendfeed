package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
)

var (
	groupAdminActor string
	groupAdminGroup string
	groupAdminUser  string
	groupAdminApply bool
)

var groupCmd = &cobra.Command{Use: "group", Short: "manage Groups"}
var groupAdminCmd = &cobra.Command{Use: "admin", Short: "manage Group administrators"}

func resolveGroupAdminProfile(ctx context.Context, identifier, profileType string) (*pb.Profile, error) {
	if identifier == "" {
		return nil, errors.New("Feed identifier is required")
	}
	response, err := apiClient.InspectFeed(ctx, &pb.InspectFeedRequest{Feed: identifier})
	if err != nil {
		return nil, err
	}
	if response.Profile == nil || response.Profile.Deleted || response.Profile.Type != profileType {
		return nil, fmt.Errorf("%q is not an active %s Feed", identifier, profileType)
	}
	return response.Profile, nil
}

func newGroupAdminCommand(name string, promote bool) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: name + " a Group administrator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			actor, err := resolveGroupAdminProfile(ctx, groupAdminActor, "user")
			if err != nil {
				return fmt.Errorf("resolve actor: %w", err)
			}
			group, err := resolveGroupAdminProfile(ctx, groupAdminGroup, "group")
			if err != nil {
				return fmt.Errorf("resolve Group: %w", err)
			}
			user, err := resolveGroupAdminProfile(ctx, groupAdminUser, "user")
			if err != nil {
				return fmt.Errorf("resolve user: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "actor: %s (%s)\nGroup: %s (%s)\nuser: %s (%s)\n",
				actor.Id, actor.Uuid, group.Id, group.Uuid, user.Id, user.Uuid)
			if !groupAdminApply {
				fmt.Fprintln(cmd.OutOrStdout(), "dry-run only; rerun with --apply to change the admin role")
				return nil
			}
			request := &pb.GroupMembershipRequest{
				ActorUuid:  actor.Uuid,
				GroupUuid:  group.Uuid,
				TargetUuid: user.Uuid,
			}
			if promote {
				_, err = apiClient.AddGroupAdmin(ctx, request)
			} else {
				_, err = apiClient.RemoveGroupAdmin(ctx, request)
			}
			if err != nil {
				return fmt.Errorf("%s Group administrator: %w", name, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), name+"d")
			return nil
		},
	}
}

var groupAdminBootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "restore the first administrator of an orphan Group",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		group, err := resolveGroupAdminProfile(ctx, groupAdminGroup, "group")
		if err != nil {
			return fmt.Errorf("resolve Group: %w", err)
		}
		user, err := resolveGroupAdminProfile(ctx, groupAdminUser, "user")
		if err != nil {
			return fmt.Errorf("resolve user: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Group: %s (%s)\nuser: %s (%s)\n",
			group.Id, group.Uuid, user.Id, user.Uuid)
		if !groupAdminApply {
			fmt.Fprintln(cmd.OutOrStdout(), "dry-run only; rerun with --apply to restore the first admin")
			return nil
		}
		_, err = apiClient.Command(ctx, &pb.CommandRequest{
			Command: "BootstrapGroupAdmin",
			Arg1:    group.Uuid,
			Arg2:    user.Uuid,
		})
		if err != nil {
			return fmt.Errorf("bootstrap Group administrator: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "bootstrapped")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(groupCmd)
	groupCmd.AddCommand(groupAdminCmd)
	groupAdminCmd.AddCommand(groupAdminBootstrapCmd)
	groupAdminBootstrapCmd.Flags().StringVar(&groupAdminGroup, "group", "", "Group ID or UUID")
	groupAdminBootstrapCmd.Flags().StringVar(&groupAdminUser, "user", "", "target user ID or UUID")
	groupAdminBootstrapCmd.Flags().BoolVar(&groupAdminApply, "apply", false, "restore the first Group administrator")
	_ = groupAdminBootstrapCmd.MarkFlagRequired("group")
	_ = groupAdminBootstrapCmd.MarkFlagRequired("user")
	for _, command := range []*cobra.Command{
		newGroupAdminCommand("promote", true),
		newGroupAdminCommand("demote", false),
	} {
		groupAdminCmd.AddCommand(command)
		command.Flags().StringVar(&groupAdminActor, "actor", "", "authorized Group admin or super ID/UUID")
		command.Flags().StringVar(&groupAdminGroup, "group", "", "Group ID or UUID")
		command.Flags().StringVar(&groupAdminUser, "user", "", "target user ID or UUID")
		command.Flags().BoolVar(&groupAdminApply, "apply", false, "apply the admin role change")
		_ = command.MarkFlagRequired("actor")
		_ = command.MarkFlagRequired("group")
		_ = command.MarkFlagRequired("user")
	}
}
