package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/protobuf/proto"
)

const systemRecordScanLimit = 100000

var errSystemScanLimit = errors.New("system stats scan limit")

type SystemReport struct {
	CollectedAt  string                   `json:"collected_at"`
	Tasks        taskqueue.Stats          `json:"tasks"`
	Services     SystemServiceReport      `json:"services"`
	FeedAPI      SystemFeedAPIReport      `json:"feed_api"`
	Timeline     SystemTimelineReport     `json:"timeline"`
	Notification SystemNotificationReport `json:"notification"`
	Public       SystemPublicReport       `json:"public_timeline"`
}

type SystemServiceReport struct {
	Active    int64 `json:"active"`
	Degraded  int64 `json:"degraded"`
	Dead      int64 `json:"dead"`
	Due       int64 `json:"due"`
	Scanned   int64 `json:"scanned"`
	Truncated bool  `json:"truncated"`
}

type SystemFeedAPIReport struct {
	Active    int64 `json:"active"`
	Revoked   int64 `json:"revoked"`
	Scanned   int64 `json:"scanned"`
	Truncated bool  `json:"truncated"`
}

type SystemTimelineReport struct {
	MaintenanceRunning int `json:"maintenance_running"`
	MaintenanceLimit   int `json:"maintenance_limit"`
	RetryBackoffs      int `json:"retry_backoffs"`
}

type SystemNotificationReport struct {
	TrimsRunning int64 `json:"trims_running"`
}

type SystemPublicReport struct {
	BumpsSinceTrim int64 `json:"bumps_since_trim"`
	TrimRunning    bool  `json:"trim_running"`
}

func (s *ApiServer) collectSystemReport(now time.Time) (SystemReport, error) {
	tasks, err := taskqueue.CollectStats(s.rdb, now)
	if err != nil {
		return SystemReport{}, fmt.Errorf("collect task stats: %w", err)
	}
	report := SystemReport{CollectedAt: now.UTC().Format(time.RFC3339Nano), Tasks: tasks}
	err = model.ServiceState.Iter(s.rdb, func(_, raw []byte) error {
		if report.Services.Scanned == systemRecordScanLimit {
			report.Services.Truncated = true
			return errSystemScanLimit
		}
		state := new(pb.ServiceState)
		if err := proto.Unmarshal(raw, state); err != nil {
			return fmt.Errorf("decode ServiceState: %w", err)
		}
		report.Services.Scanned++
		switch state.Status {
		case "", model.ServiceStatusActive:
			report.Services.Active++
		case model.ServiceStatusDegraded:
			report.Services.Degraded++
		case model.ServiceStatusDead:
			report.Services.Dead++
		}
		if state.Status != model.ServiceStatusDead && state.NextFetchMs <= now.UTC().UnixMilli() {
			report.Services.Due++
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSystemScanLimit) {
		return SystemReport{}, fmt.Errorf("collect service stats: %w", err)
	}
	err = model.FeedApiKey.Iter(s.rdb, func(_, raw []byte) error {
		if report.FeedAPI.Scanned == systemRecordScanLimit {
			report.FeedAPI.Truncated = true
			return errSystemScanLimit
		}
		record := new(pb.FeedApiKeyRecord)
		if err := proto.Unmarshal(raw, record); err != nil {
			return fmt.Errorf("decode FeedApiKeyRecord: %w", err)
		}
		report.FeedAPI.Scanned++
		switch {
		case record.RevokedAtMs == 0 && len(record.KeyId) == 8 && len(record.SecretSha256) == 32:
			report.FeedAPI.Active++
		case record.RevokedAtMs > 0 && len(record.KeyId) == 8 && len(record.SecretSha256) == 0:
			report.FeedAPI.Revoked++
		default:
			return errors.New("invalid FeedApiKeyRecord lifecycle encoding")
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSystemScanLimit) {
		return SystemReport{}, fmt.Errorf("collect Feed API stats: %w", err)
	}
	report.Timeline.MaintenanceRunning = len(s.timelineBuildSlots)
	report.Timeline.MaintenanceLimit = cap(s.timelineBuildSlots)
	s.timelineFailureMu.Lock()
	for _, retryAt := range s.timelineRetryAfter {
		if retryAt.After(now) {
			report.Timeline.RetryBackoffs++
		}
	}
	s.timelineFailureMu.Unlock()
	report.Notification.TrimsRunning = s.notificationTrims.Load()
	report.Public.BumpsSinceTrim = s.publicTimelineBumps.Load()
	report.Public.TrimRunning = s.publicTimelineTrimming.Load()
	return report, nil
}

func marshalSystemReport(report SystemReport) (string, error) {
	data, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("encode system report: %w", err)
	}
	return string(data), nil
}
