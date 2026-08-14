package model

import (
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/store"
)

// PublicTimelineUUID is the reserved viewer UUID of the shared public
// timeline. It is not a real profile: it never appears in Follow/Follower,
// never receives Home fanout and never gets a TimelineState row.
var PublicTimelineUUID = uuid.NewV5(uuid.NamespaceURL, "timeline:public")

// PublicTimelineMaxEntries bounds the public timeline independently of the
// per-viewer Home limit (TimelineMaxEntries).
const PublicTimelineMaxEntries = 10_000

// IsPublicTimeline reports whether viewer is the reserved public timeline
// viewer. Callers use it to bypass TimelineState activity checks: the public
// timeline is always active and never decays to the cold cache.
func IsPublicTimeline(viewer uuid.UUID) bool {
	return viewer == PublicTimelineUUID
}

// BumpPublicTimeline inserts or moves an entry in the public timeline with
// activity as its event time. Unlike Home fanout there is no activity
// qualification (no Like window or cooldown): callers decide whether the
// triggering event deserves a bump — only new entries, first Likes and new
// Comments of non-private feeds may call this.
func BumpPublicTimeline(db *store.Store, entry uuid.UUID, activity time.Time) error {
	_, err := MoveTimelineEntry(db, PublicTimelineUUID, entry, activity, nil)
	return err
}

// TrimPublicTimeline keeps the newest PublicTimelineMaxEntries rows of the
// public timeline, deleting Index and Position in pairs. It runs from a
// background ticker, never inside a request path.
func TrimPublicTimeline(db *store.Store, now time.Time) (int, error) {
	return TrimHomeTimeline(db, PublicTimelineUUID, PublicTimelineMaxEntries, TimelineRetentionMax, now)
}

// BuildPublicTimeline selects the globally newest publications across the
// given non-private feeds. The initial activity of a backfilled row is its
// publish time: historical push order has no event log and cannot be
// recovered. Live bumps take over ordering after the cutover.
func BuildPublicTimeline(db *store.Store, feeds []uuid.UUID, maxRows int, retention time.Duration, now time.Time) (map[uuid.UUID]time.Time, error) {
	if maxRows <= 0 {
		maxRows = PublicTimelineMaxEntries
	}
	return SelectTimelineCandidates(db, feeds, maxRows, retention, now)
}

// ReplacePublicTimeline atomically replaces the public timeline with rows.
// The public viewer has no TimelineState to refresh.
func ReplacePublicTimeline(db *store.Store, rows map[uuid.UUID]time.Time) error {
	return ReplaceHomeTimeline(db, PublicTimelineUUID, rows)
}
