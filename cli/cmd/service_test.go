package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinServiceAddCommandIsRegistered(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"service", "add"})
	require.NoError(t, err)
	require.Equal(t, "add", command.Name())
	for _, name := range []string{"feed", "kind", "apply"} {
		require.NotNil(t, command.Flags().Lookup(name))
	}
}
