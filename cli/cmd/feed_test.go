package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeedPrivacyIdentifiersCombinesFileAndFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feeds.txt")
	require.NoError(t, os.WriteFile(path, []byte("# batch\nalice\n\nbob\n"), 0o600))
	feeds, err := feedPrivacyIdentifiers([]string{"alice", "carol"}, path)
	require.NoError(t, err)
	require.Equal(t, []string{"alice", "carol", "bob"}, feeds)
}

func TestFeedPrivacyIdentifiersRequiresBoundedInput(t *testing.T) {
	_, err := feedPrivacyIdentifiers(nil, "")
	require.Error(t, err)

	feeds := make([]string, 101)
	for i := range feeds {
		feeds[i] = fmt.Sprintf("feed-%d", i)
	}
	_, err = feedPrivacyIdentifiers(feeds, "")
	require.ErrorContains(t, err, "at most 100")
}
