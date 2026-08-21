package server

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

// publicTimelineTrimEvery is the bump budget that arms a background trim.
// The public timeline may overshoot PublicTimelineMaxEntries by at most this
// many rows between trims.
const publicTimelineTrimEvery = 100

// isPublicFeedRequest reports whether req targets the shared public timeline.
// Public has no profile UUID of its own on the wire; it is addressed by the
// reserved feed ID "public".
func isPublicFeedRequest(req *pb.FeedRequest) bool {
	return req.ProfileUuid == "" && req.Id == "public"
}

// entryCreated reports whether the entry key is absent before PutEntry. The
// check deliberately runs outside the PutEntry batch: losing a race against a
// concurrent archive of the same new entry only costs one extra idempotent
// public bump.
func (s *ApiServer) entryCreated(entry *pb.Entry) (bool, error) {
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return false, err
	}
	exists, err := s.rdb.Exists(model.Entry.PrefixAppend(entryUUID.Bytes()))
	return !exists, err
}

// likeCreated reports whether the profile has not liked the entry yet. Like
// the entryCreated pre-check, a lost race only costs a harmless extra bump.
func (s *ApiServer) likeCreated(entry *pb.Entry, profile *pb.Profile) (bool, error) {
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return false, err
	}
	actorUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return false, err
	}
	exists, err := s.rdb.Exists(model.LikeKey(entryUUID, actorUUID))
	return !exists, err
}

// commentCreated reports whether the comment key is absent, i.e. the request
// creates a new comment rather than editing an existing one.
func (s *ApiServer) commentCreated(entry *pb.Entry, comment *pb.Comment) (bool, error) {
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return false, err
	}
	commentUUID, err := uuid.FromString(comment.Id)
	if err != nil {
		return false, err
	}
	exists, err := s.rdb.Exists(model.CommentKey(entryUUID, commentUUID))
	return !exists, err
}

// bumpPublicTimeline inserts or moves an entry in the shared public timeline.
// Only callers that established the event creates new content (new entry,
// first Like, new Comment) may call it. Entries whose target feed is private,
// deleted or unresolvable never enter public. privateFeeds caches the privacy
// verdict per FeedUuid within an archive stream; pass nil for one-shot calls.
func (s *ApiServer) bumpPublicTimeline(entry *pb.Entry, privateFeeds map[string]bool) error {
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return err
	}
	// Like/Comment notifications are staged atomically in model.PutLike and
	// model.PutComment. This callback happens after those commits even for a
	// private target, so use it to arm the same bounded retention maintenance
	// as server-owned direct notification stages. New Entry bumps simply find
	// no over-threshold NotificationState and are a cheap no-op.
	if recipient, err := uuid.FromString(entry.ProfileUuid); err == nil && recipient != uuid.Nil {
		s.scheduleNotificationTrimIfNeeded(recipient)
	}
	feedUUID := entry.FeedUuid
	if feedUUID == "" {
		feedUUID = entry.ProfileUuid // same default as model.PutEntry
	}
	private, cached := privateFeeds[feedUUID]
	if !cached {
		feedID, err := uuid.FromString(feedUUID)
		if err != nil {
			return err
		}
		profile, err := model.GetProfileFromUuid(s.mdb, feedID)
		switch {
		case errors.Is(err, model.ErrNotFound), errors.Is(err, model.ErrProfileDeleted):
			// An unresolvable target feed must not leak into public.
			private = true
		case err != nil:
			return err
		default:
			private = profile.Private
		}
		if privateFeeds != nil {
			privateFeeds[feedUUID] = private
		}
	}
	if private {
		return nil
	}
	if err := model.BumpPublicTimeline(s.rdb, entryUUID, time.Now().UTC()); err != nil {
		return err
	}
	if s.publicTimelineBumps.Add(1) >= publicTimelineTrimEvery {
		s.schedulePublicTimelineTrim()
	}
	return nil
}

// schedulePublicTimelineTrim runs a bounded trim in the background once the
// bump budget is exhausted. At most one trim runs at a time; bumps arriving
// during a trim are accounted by the loop re-check. The goroutine is tracked
// by the server lifecycle WaitGroup and drained before the database closes.
func (s *ApiServer) schedulePublicTimelineTrim() {
	if !s.publicTimelineTrimming.CompareAndSwap(false, true) {
		return
	}
	if !s.beginBackgroundJob() {
		s.publicTimelineTrimming.Store(false)
		return
	}
	go func() {
		defer s.wg.Done()
		defer s.publicTimelineTrimming.Store(false)
		for {
			s.publicTimelineBumps.Store(0)
			if _, err := model.TrimPublicTimeline(s.rdb, time.Now().UTC()); err != nil {
				slog.Error("trim public timeline", "err", err)
				return
			}
			if s.publicTimelineBumps.Load() < publicTimelineTrimEvery {
				return
			}
		}
	}()
}
