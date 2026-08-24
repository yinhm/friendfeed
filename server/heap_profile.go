package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
	"time"
)

const (
	runtimeProfileDir   = "/tmp/ffdb-diagnostics"
	runtimeProfileLimit = 3
)

var heapProfileMu sync.Mutex

func captureRuntimeHeapProfile(dir string) (string, error) {
	if !heapProfileMu.TryLock() {
		return "", errors.New("heap profile capture already in progress")
	}
	defer heapProfileMu.Unlock()

	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("create diagnostics directory: %w", err)
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("inspect diagnostics directory: %w", err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("diagnostics path is not a real directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("protect diagnostics directory: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("list diagnostics directory: %w", err)
	}
	profiles := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "heap-") && strings.HasSuffix(entry.Name(), ".pprof") {
			profiles++
		}
	}
	if profiles >= runtimeProfileLimit {
		return "", fmt.Errorf("heap profile limit reached (%d); remove an old profile from %s", runtimeProfileLimit, dir)
	}

	name := fmt.Sprintf("heap-%s-*.pprof", time.Now().UTC().Format("20060102T150405Z"))
	file, err := os.CreateTemp(dir, name)
	if err != nil {
		return "", fmt.Errorf("create heap profile: %w", err)
	}
	path := file.Name()
	remove := true
	defer func() {
		file.Close()
		if remove {
			os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", fmt.Errorf("protect heap profile: %w", err)
	}
	// WriteHeapProfile captures the current Go heap. It does not trigger the
	// forced-GC/cache-eviction behavior this diagnostic is intended to avoid.
	if err := pprof.WriteHeapProfile(file); err != nil {
		return "", fmt.Errorf("write heap profile: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close heap profile: %w", err)
	}
	remove = false
	return filepath.Clean(path), nil
}
