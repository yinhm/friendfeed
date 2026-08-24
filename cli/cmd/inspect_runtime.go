package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
)

var runtimeInspectJSON bool

type runtimeInspectReport struct {
	CollectedAt string `json:"collected_at"`
	Process     struct {
		RSSBytes     uint64 `json:"rss_bytes"`
		VirtualBytes uint64 `json:"virtual_bytes"`
		Threads      uint64 `json:"threads"`
		OpenFDs      int    `json:"open_fds"`
	} `json:"process"`
	Go struct {
		HeapAllocBytes    uint64 `json:"heap_alloc_bytes"`
		HeapInuseBytes    uint64 `json:"heap_inuse_bytes"`
		HeapSysBytes      uint64 `json:"heap_sys_bytes"`
		HeapIdleBytes     uint64 `json:"heap_idle_bytes"`
		HeapReleasedBytes uint64 `json:"heap_released_bytes"`
		StackInuseBytes   uint64 `json:"stack_inuse_bytes"`
		OtherSysBytes     uint64 `json:"other_sys_bytes"`
		NextGCBytes       uint64 `json:"next_gc_bytes"`
		MemoryLimitBytes  int64  `json:"memory_limit_bytes"`
		NumGC             uint32 `json:"num_gc"`
		Goroutines        int    `json:"goroutines"`
		GOMAXPROCS        int    `json:"gomaxprocs"`
	} `json:"go"`
	Pebble struct {
		BlockCacheBytes      int64  `json:"block_cache_bytes"`
		BlockCacheLimitBytes int64  `json:"block_cache_limit_bytes"`
		BlockCacheEntries    int64  `json:"block_cache_entries"`
		BlockCacheHits       int64  `json:"block_cache_hits"`
		BlockCacheMisses     int64  `json:"block_cache_misses"`
		MemTableBytes        uint64 `json:"memtable_bytes"`
		MemTableCount        int64  `json:"memtable_count"`
		ZombieMemTableBytes  uint64 `json:"zombie_memtable_bytes"`
		ZombieMemTableCount  int64  `json:"zombie_memtable_count"`
		ZombieTableBytes     uint64 `json:"zombie_table_bytes"`
		ZombieTableCount     int64  `json:"zombie_table_count"`
		OpenTableIterators   int64  `json:"open_table_iterators"`
		OpenSnapshots        int    `json:"open_snapshots"`
		FileCacheBytes       int64  `json:"file_cache_bytes"`
		FileCacheTables      int64  `json:"file_cache_tables"`
		DiskUsageBytes       uint64 `json:"disk_usage_bytes"`
		CompactionDebtBytes  uint64 `json:"compaction_debt_bytes"`
		CompactionsRunning   int64  `json:"compactions_running"`
		FlushesRunning       int64  `json:"flushes_running"`
		UptimeSeconds        int64  `json:"uptime_seconds"`
	} `json:"pebble"`
	Realtime struct {
		Subscribers  int    `json:"subscribers"`
		DroppedHints uint64 `json:"dropped_hints"`
	} `json:"realtime"`
}

var inspectRuntimeCmd = &cobra.Command{
	Use:   "inspect-runtime",
	Short: "inspect the running ffdb process without scanning data",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		response, err := agent.client.Command(ctx, &pb.CommandRequest{Command: "RuntimeInspect"})
		if err != nil {
			return fmt.Errorf("inspect ffdb runtime: %w", err)
		}
		var report runtimeInspectReport
		if err := json.Unmarshal([]byte(response.Result), &report); err != nil {
			return fmt.Errorf("decode ffdb runtime report: %w", err)
		}
		if runtimeInspectJSON {
			var formatted any
			if err := json.Unmarshal([]byte(response.Result), &formatted); err != nil {
				return err
			}
			data, err := json.MarshalIndent(formatted, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		writeRuntimeInspectReport(cmd.OutOrStdout(), report)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(inspectRuntimeCmd)
	inspectRuntimeCmd.Flags().BoolVar(&runtimeInspectJSON, "json", false, "print the complete report as JSON")
}

func writeRuntimeInspectReport(w io.Writer, r runtimeInspectReport) {
	fmt.Fprintf(w, "collected: %s\n", r.CollectedAt)
	fmt.Fprintf(w, "process:   RSS %s, virtual %s, threads %d, fds %d\n",
		formatBytes(r.Process.RSSBytes), formatBytes(r.Process.VirtualBytes), r.Process.Threads, r.Process.OpenFDs)
	fmt.Fprintf(w, "go heap:   alloc %s, in-use %s, sys %s, idle %s, released %s\n",
		formatBytes(r.Go.HeapAllocBytes), formatBytes(r.Go.HeapInuseBytes), formatBytes(r.Go.HeapSysBytes),
		formatBytes(r.Go.HeapIdleBytes), formatBytes(r.Go.HeapReleasedBytes))
	fmt.Fprintf(w, "go runtime: stack %s, other-sys %s, next-gc %s, limit %s, goroutines %d, gc %d, gomaxprocs %d\n",
		formatBytes(r.Go.StackInuseBytes), formatBytes(r.Go.OtherSysBytes), formatBytes(r.Go.NextGCBytes),
		formatSignedBytes(r.Go.MemoryLimitBytes), r.Go.Goroutines, r.Go.NumGC, r.Go.GOMAXPROCS)
	fmt.Fprintf(w, "pebble cache: %s / %s, entries %d, hits %d, misses %d\n",
		formatSignedBytes(r.Pebble.BlockCacheBytes), formatSignedBytes(r.Pebble.BlockCacheLimitBytes),
		r.Pebble.BlockCacheEntries, r.Pebble.BlockCacheHits, r.Pebble.BlockCacheMisses)
	fmt.Fprintf(w, "pebble memory: memtable %s (%d), zombie memtable %s (%d), zombie table %s (%d)\n",
		formatBytes(r.Pebble.MemTableBytes), r.Pebble.MemTableCount,
		formatBytes(r.Pebble.ZombieMemTableBytes), r.Pebble.ZombieMemTableCount,
		formatBytes(r.Pebble.ZombieTableBytes), r.Pebble.ZombieTableCount)
	fmt.Fprintf(w, "pebble state: iterators %d, snapshots %d, file-cache %s (%d tables), disk %s, debt %s, compacting %d, flushing %d, uptime %s\n",
		r.Pebble.OpenTableIterators, r.Pebble.OpenSnapshots, formatSignedBytes(r.Pebble.FileCacheBytes),
		r.Pebble.FileCacheTables, formatBytes(r.Pebble.DiskUsageBytes), formatBytes(r.Pebble.CompactionDebtBytes),
		r.Pebble.CompactionsRunning, r.Pebble.FlushesRunning, (time.Duration(r.Pebble.UptimeSeconds) * time.Second).String())
	fmt.Fprintf(w, "realtime: subscribers %d, dropped hints %d\n", r.Realtime.Subscribers, r.Realtime.DroppedHints)
}

func formatSignedBytes(value int64) string {
	if value < 0 || value == math.MaxInt64 {
		return "unlimited"
	}
	return formatBytes(uint64(value))
}

func formatBytes(value uint64) string {
	const unit = uint64(1024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := unit, 0
	for n := value / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
