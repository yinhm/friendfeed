package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCommandUsesDevelopmentRPCByDefault(t *testing.T) {
	address := rootCmd.PersistentFlags().Lookup("address")
	require.NotNil(t, address)
	require.Equal(t, "localhost:3000", address.DefValue)
	require.Nil(t, rootCmd.PersistentFlags().Lookup("path"))
}
