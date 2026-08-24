package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteRuntimeInspectReport(t *testing.T) {
	var report runtimeInspectReport
	report.CollectedAt = "2026-08-24T12:00:00Z"
	report.Process.RSSBytes = 2 << 30
	report.Go.HeapAllocBytes = 256 << 20
	report.Pebble.BlockCacheBytes = 128 << 20
	report.Pebble.BlockCacheLimitBytes = 512 << 20
	report.Pebble.ZombieMemTableBytes = 64 << 20
	report.Realtime.DroppedHints = 3

	var out bytes.Buffer
	writeRuntimeInspectReport(&out, report)
	for _, want := range []string{
		"RSS 2.0 GiB", "alloc 256.0 MiB", "128.0 MiB / 512.0 MiB",
		"zombie memtable 64.0 MiB", "dropped hints 3",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output %q does not contain %q", out.String(), want)
		}
	}
}

func TestFormatSignedBytesUnlimited(t *testing.T) {
	if got := formatSignedBytes(-1); got != "unlimited" {
		t.Fatalf("formatSignedBytes(-1) = %q", got)
	}
}
