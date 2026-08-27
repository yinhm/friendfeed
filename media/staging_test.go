package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/util"
)

func TestStagingPromoteVerifiesAndPublishesCanonicalObject(t *testing.T) {
	root := t.TempDir()
	store := NewStagingStore(&util.Config{MediaPath: root})
	content := []byte("canonical content")
	name, digest, err := store.Put(content, "pdf")
	require.NoError(t, err)
	key, err := store.Promote(name, digest, "pdf", len(content))
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(key, ".pdf"))
	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(key)))
	require.NoError(t, err)
	require.Equal(t, content, stored)

	_, err = store.Promote(name, strings.Repeat("0", 64), "pdf", len(content))
	require.Error(t, err)
}

func TestStagingCleanupOnlyRemovesExpiredRegularObjects(t *testing.T) {
	root := t.TempDir()
	store := NewStagingStore(&util.Config{MediaPath: root})
	old, _, err := store.Put([]byte("old"), "txt")
	require.NoError(t, err)
	recent, _, err := store.Put([]byte("recent"), "txt")
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, os.Chtimes(filepath.Join(root, StagingDirectory, old), now.Add(-25*time.Hour), now.Add(-25*time.Hour)))
	removed, err := store.Cleanup(now, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	_, err = os.Stat(filepath.Join(root, StagingDirectory, recent))
	require.NoError(t, err)
}

func TestCanonicalKeyFromURLRejectsForeignAndMalformedObjects(t *testing.T) {
	key := "a/b/" + strings.Repeat("c", 62) + ".jpg"
	got, ok := CanonicalKeyFromURL("https://m.example", "https://m.example/"+key+"?download=x")
	require.True(t, ok)
	require.Equal(t, key, got)
	_, ok = CanonicalKeyFromURL("https://m.example", "https://other.example/"+key)
	require.False(t, ok)
}
