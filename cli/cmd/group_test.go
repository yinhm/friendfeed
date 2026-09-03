package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupAdminCommandsAreRegistered(t *testing.T) {
	for _, action := range []string{"promote", "demote"} {
		command, _, err := rootCmd.Find([]string{"group", "admin", action})
		require.NoError(t, err)
		require.Equal(t, action, command.Name())
		for _, flag := range []string{"actor", "group", "user", "apply"} {
			require.NotNil(t, command.Flags().Lookup(flag))
		}
	}
}
