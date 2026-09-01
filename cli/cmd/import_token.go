package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	importTokenTTL time.Duration
	importTokenOut string
)

var importTokenCmd = &cobra.Command{
	Use:   "import-token",
	Short: "manage the short-lived site-wide import operator token",
}

var importTokenIssueCmd = &cobra.Command{
	Use:   "issue",
	Short: "issue a token and write it once to a new 0600 file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if importTokenOut == "" {
			return errors.New("--out is required")
		}
		if importTokenTTL < time.Second || importTokenTTL > time.Hour {
			return errors.New("--ttl must be between 1s and 1h")
		}
		file, err := os.OpenFile(importTokenOut, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("create token file: %w", err)
		}
		keepFile := false
		defer func() {
			_ = file.Close()
			if !keepFile {
				_ = os.Remove(importTokenOut)
			}
		}()
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		response, err := apiClient.IssueImportOperatorToken(ctx, &pb.IssueImportOperatorTokenRequest{
			TtlSeconds: int64(importTokenTTL / time.Second), IssuedBy: localOperatorIdentity(),
		})
		if err != nil {
			_, _ = apiClient.RevokeImportOperatorToken(ctx, &emptypb.Empty{})
			return fmt.Errorf("issue import operator token: %w", err)
		}
		if response == nil || response.Status == nil || response.Token == "" {
			_, _ = apiClient.RevokeImportOperatorToken(ctx, &emptypb.Empty{})
			return errors.New("issue import operator token returned an invalid response")
		}
		if _, err = fmt.Fprintln(file, response.Token); err == nil {
			err = file.Close()
		}
		if err != nil {
			_, _ = apiClient.RevokeImportOperatorToken(ctx, &emptypb.Empty{})
			return fmt.Errorf("write token file: %w", err)
		}
		keepFile = true
		fmt.Fprintf(cmd.OutOrStdout(), "issued import operator token; expires %s; key written to %s\n",
			time.UnixMilli(response.Status.ExpiresAtMs).UTC().Format(time.RFC3339), importTokenOut)
		return nil
	},
}

func localOperatorIdentity() string {
	name := os.Getenv("USER")
	if current, err := user.Current(); err == nil && current.Username != "" {
		name = current.Username
	}
	host, _ := os.Hostname()
	if name == "" {
		name = "unknown"
	}
	if host == "" {
		host = "localhost"
	}
	clean := func(raw string) string {
		return strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r) {
				return r
			}
			return '_'
		}, raw)
	}
	return clean(name) + "@" + clean(host)
}

var importTokenInspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "show token metadata without exposing its secret",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		response, err := apiClient.GetImportOperatorTokenStatus(ctx, &emptypb.Empty{})
		if err != nil {
			return fmt.Errorf("inspect import operator token: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "active=%t created=%s expires=%s revoked=%s\n", response.Active,
			formatTokenTime(response.CreatedAtMs), formatTokenTime(response.ExpiresAtMs), formatTokenTime(response.RevokedAtMs))
		return nil
	},
}

var importTokenRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "revoke the active import operator token",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		response, err := apiClient.RevokeImportOperatorToken(ctx, &emptypb.Empty{})
		if err != nil {
			return fmt.Errorf("revoke import operator token: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "active=%t revoked=%s\n", response.Active, formatTokenTime(response.RevokedAtMs))
		return nil
	},
}

func formatTokenTime(milliseconds int64) string {
	if milliseconds == 0 {
		return "-"
	}
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339)
}

func init() {
	rootCmd.AddCommand(importTokenCmd)
	importTokenCmd.AddCommand(importTokenIssueCmd, importTokenInspectCmd, importTokenRevokeCmd)
	importTokenIssueCmd.Flags().DurationVar(&importTokenTTL, "ttl", time.Hour, "token lifetime (maximum 1h)")
	importTokenIssueCmd.Flags().StringVar(&importTokenOut, "out", "", "new file to receive the token (required, mode 0600)")
}
