package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/yinhm/friendfeed/store"
)

// RuntimeReport is a bounded, content-free snapshot for diagnosing ffdb
// memory pressure. All byte fields are raw byte counts.
type RuntimeReport struct {
	CollectedAt string         `json:"collected_at"`
	Process     ProcessReport  `json:"process"`
	Go          GoReport       `json:"go"`
	Pebble      PebbleReport   `json:"pebble"`
	Realtime    RealtimeReport `json:"realtime"`
}

type ProcessReport struct {
	RSSBytes     uint64 `json:"rss_bytes"`
	VirtualBytes uint64 `json:"virtual_bytes"`
	Threads      uint64 `json:"threads"`
	OpenFDs      int    `json:"open_fds"`
}

type GoReport struct {
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
}

type PebbleReport struct {
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
}

type RealtimeReport struct {
	Subscribers  int    `json:"subscribers"`
	DroppedHints uint64 `json:"dropped_hints"`
}

func collectRuntimeReport(db *store.Store, bus *realtimeBus) (RuntimeReport, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	report := RuntimeReport{
		CollectedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Go: GoReport{
			HeapAllocBytes: mem.HeapAlloc, HeapInuseBytes: mem.HeapInuse,
			HeapSysBytes: mem.HeapSys, HeapIdleBytes: mem.HeapIdle,
			HeapReleasedBytes: mem.HeapReleased, StackInuseBytes: mem.StackInuse,
			OtherSysBytes: mem.OtherSys, NextGCBytes: mem.NextGC,
			MemoryLimitBytes: debug.SetMemoryLimit(-1), NumGC: mem.NumGC,
			Goroutines: runtime.NumGoroutine(), GOMAXPROCS: runtime.GOMAXPROCS(0),
		},
	}

	process, err := readLinuxProcessReport()
	if err != nil {
		return RuntimeReport{}, err
	}
	report.Process = process

	metrics := db.Metrics()
	report.Pebble = PebbleReport{
		BlockCacheBytes:      metrics.BlockCache.Size,
		BlockCacheLimitBytes: db.Options().Cache.MaxSize(),
		BlockCacheEntries:    metrics.BlockCache.Count,
		BlockCacheHits:       metrics.BlockCache.Hits,
		BlockCacheMisses:     metrics.BlockCache.Misses,
		MemTableBytes:        metrics.MemTable.Size,
		MemTableCount:        metrics.MemTable.Count,
		ZombieMemTableBytes:  metrics.MemTable.ZombieSize,
		ZombieMemTableCount:  metrics.MemTable.ZombieCount,
		ZombieTableBytes:     metrics.Table.ZombieSize,
		ZombieTableCount:     metrics.Table.ZombieCount,
		OpenTableIterators:   metrics.TableIters,
		OpenSnapshots:        metrics.Snapshots.Count,
		FileCacheBytes:       metrics.FileCache.Size,
		FileCacheTables:      metrics.FileCache.TableCount,
		DiskUsageBytes:       metrics.DiskSpaceUsage(),
		CompactionDebtBytes:  metrics.Compact.EstimatedDebt,
		CompactionsRunning:   metrics.Compact.NumInProgress,
		FlushesRunning:       metrics.Flush.NumInProgress,
		UptimeSeconds:        int64(metrics.Uptime / time.Second),
	}
	if bus != nil {
		bus.mu.Lock()
		report.Realtime.Subscribers = len(bus.subscribers)
		bus.mu.Unlock()
		report.Realtime.DroppedHints = bus.dropCount()
	}
	return report, nil
}

func readLinuxProcessReport() (ProcessReport, error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return ProcessReport{}, fmt.Errorf("open process status: %w", err)
	}
	defer f.Close()

	var report ProcessReport
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "VmRSS:":
			report.RSSBytes, err = parseProcKB(fields)
		case "VmSize:":
			report.VirtualBytes, err = parseProcKB(fields)
		case "Threads:":
			report.Threads, err = strconv.ParseUint(fields[1], 10, 64)
		}
		if err != nil {
			return ProcessReport{}, fmt.Errorf("parse %s: %w", fields[0], err)
		}
	}
	if err := scanner.Err(); err != nil {
		return ProcessReport{}, fmt.Errorf("read process status: %w", err)
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return ProcessReport{}, fmt.Errorf("read process descriptors: %w", err)
	}
	report.OpenFDs = len(entries)
	return report, nil
}

func parseProcKB(fields []string) (uint64, error) {
	value, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return value * 1024, nil
}

func marshalRuntimeReport(report RuntimeReport) (string, error) {
	data, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("encode runtime report: %w", err)
	}
	return string(data), nil
}
