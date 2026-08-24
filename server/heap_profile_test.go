package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaptureRuntimeHeapProfileUsesPrivateBoundedFiles(t *testing.T) {
	dir := t.TempDir()
	path, err := captureRuntimeHeapProfile(dir)
	require.NoError(t, err)
	require.Equal(t, dir, filepath.Dir(path))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.Positive(t, info.Size())

	for i := 1; i < runtimeProfileLimit; i++ {
		_, err := captureRuntimeHeapProfile(dir)
		require.NoError(t, err)
	}
	_, err = captureRuntimeHeapProfile(dir)
	require.ErrorContains(t, err, "profile limit reached")
}
