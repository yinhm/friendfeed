package model

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/store"
)

// FollowRequestKey encodes a pending follow request: target feed UUID
// followed by requester user UUID. The value is the RFC3339 request time.
func FollowRequestKey(target, requester uuid.UUID) (store.Key, error) {
	if target == uuid.Nil || requester == uuid.Nil {
		return nil, errors.New("target feed UUID and requester user UUID are required")
	}
	return NewKeyFrom(FollowRequest.Prefix, target.Bytes(), requester.Bytes()), nil
}

// IsFollowRequestPending reports whether requester has a pending follow
// request against target.
func IsFollowRequestPending(db *store.Store, target, requester uuid.UUID) (bool, error) {
	key, err := FollowRequestKey(target, requester)
	if err != nil {
		return false, err
	}
	return db.Exists(key)
}

// IsFollower reports whether user currently follows feed. Subscribing to a
// user feed and joining a Group are the same Follow edge (docs/group.md);
// IsGroupMember is the Group-named specialization of this check.
func IsFollower(db *store.Store, feed, user uuid.UUID) (bool, error) {
	if feed == uuid.Nil || user == uuid.Nil {
		return false, errors.New("feed UUID and user UUID are required")
	}
	return db.Exists(NewKeyFrom(Follow.Prefix, user.Bytes(), feed.Bytes()))
}

// ErrFollowTargetNotPrivate is returned when a follow request is staged
// against a target that does not require approval; public targets take the
// direct edge-writing path instead.
var ErrFollowTargetNotPrivate = errors.New("follow requests only apply to private feeds")

// ErrFollowRequestNotFound is returned when approving a request that does
// not exist and whose requester is not already a follower.
var ErrFollowRequestNotFound = errors.New("follow request not found")

// StageRequestFollow stages a pending follow request against a private
// target (user feed or Group). It is idempotent: an existing request keeps
// its original timestamp, and an existing follower needs no request.
func StageRequestFollow(db *store.Store, batch *pebble.Batch, target, requester uuid.UUID, now time.Time) error {
	if batch == nil || target == uuid.Nil || requester == uuid.Nil {
		return errors.New("batch, target feed UUID, and requester UUID are required")
	}
	targetProfile, err := GetProfileFromUuid(db, target)
	if err != nil {
		return err
	}
	if !targetProfile.Private {
		return ErrFollowTargetNotPrivate
	}
	if _, err := getGroupMemberProfile(db, requester); err != nil {
		return err
	}
	following, err := IsFollower(db, target, requester)
	if err != nil {
		return err
	}
	if following {
		return nil
	}
	pending, err := IsFollowRequestPending(db, target, requester)
	if err != nil {
		return err
	}
	if pending {
		return nil
	}
	key, err := FollowRequestKey(target, requester)
	if err != nil {
		return err
	}
	return batch.Set(key, []byte(now.UTC().Format(time.RFC3339)), nil)
}

// StageApproveFollowRequest converts a pending request into the actual
// Follow/Follower edge pair, deleting the request in the same batch. The
// approval itself is the authorization, so this path deliberately skips the
// private-Group rejection StageJoinGroup enforces on direct joins. It is
// idempotent for an already-following requester (any stale request is
// cleaned up); approving with neither a request nor an existing edge fails
// with ErrFollowRequestNotFound.
//
// Callers must also enqueue the requester's home rebuild in the same batch,
// per docs/group.md's timeline rule.
func StageApproveFollowRequest(db *store.Store, batch *pebble.Batch, target, requester uuid.UUID) error {
	if batch == nil || target == uuid.Nil || requester == uuid.Nil {
		return errors.New("batch, target feed UUID, and requester UUID are required")
	}
	targetProfile, err := GetProfileFromUuid(db, target)
	if err != nil {
		return err
	}
	if _, err := getGroupMemberProfile(db, requester); err != nil {
		return err
	}
	following, err := IsFollower(db, target, requester)
	if err != nil {
		return err
	}
	if following {
		return StageDeleteFollowRequest(db, batch, target, requester)
	}
	pending, err := IsFollowRequestPending(db, target, requester)
	if err != nil {
		return err
	}
	if !pending {
		return ErrFollowRequestNotFound
	}
	if err := StageDeleteFollowRequest(db, batch, target, requester); err != nil {
		return err
	}
	if targetProfile.Type == "group" {
		return stageGroupJoinEdges(db, batch, target, requester)
	}
	return stageFollowEdges(db, batch, target, requester)
}

// StageDeleteFollowRequest removes a pending request. It backs both the
// requester's own cancel and the approver's reject, and is idempotent.
func StageDeleteFollowRequest(db *store.Store, batch *pebble.Batch, target, requester uuid.UUID) error {
	if batch == nil || target == uuid.Nil || requester == uuid.Nil {
		return errors.New("batch, target feed UUID, and requester UUID are required")
	}
	key, err := FollowRequestKey(target, requester)
	if err != nil {
		return err
	}
	return batch.Delete(key, nil)
}

// stageFollowEdges writes the plain Follow/Follower pair of a user-feed
// subscription. Group targets need stageGroupJoinEdges instead, which also
// maintains the activity index. Any stale follow request for the pair is
// cleared in the same batch: an established or explicitly removed
// relationship must never let an old request resurface later.
func stageFollowEdges(db *store.Store, batch *pebble.Batch, feed, user uuid.UUID) error {
	if err := batch.Set(NewKeyFrom(Follow.Prefix, user.Bytes(), feed.Bytes()), []byte("1"), nil); err != nil {
		return fmt.Errorf("stage Follow: %w", err)
	}
	if err := batch.Set(NewKeyFrom(Follower.Prefix, feed.Bytes(), user.Bytes()), []byte("1"), nil); err != nil {
		return fmt.Errorf("stage Follower: %w", err)
	}
	return StageDeleteFollowRequest(db, batch, feed, user)
}

// FollowRequestEntry is one pending request returned by ListFollowRequests.
type FollowRequestEntry struct {
	Requester   uuid.UUID
	RequestedAt string
}

// ListFollowRequests streams the pending requests against target in key
// order. cursor marks an already-returned requester to continue after; the
// returned next cursor is empty once the scan is exhausted. Same cursor
// discipline as ListGroupMembers.
func ListFollowRequests(db *store.Store, target uuid.UUID, limit int, cursor uuid.UUID) ([]FollowRequestEntry, string, error) {
	if target == uuid.Nil {
		return nil, "", errors.New("target feed UUID is required")
	}
	if limit <= 0 {
		limit = 100
	}
	prefix := NewKeyFrom(FollowRequest.Prefix, target.Bytes())
	iter, err := db.NewIterator(prefix)
	if err != nil {
		return nil, "", err
	}
	defer iter.Close()

	if cursor != uuid.Nil {
		cursorKey := NewKeyFrom(FollowRequest.Prefix, target.Bytes(), cursor.Bytes())
		iter.SeekGE(cursorKey)
		if iter.Valid() && bytes.Equal(iter.Key(), cursorKey) {
			iter.Next()
		}
	} else {
		iter.First()
	}

	var requests []FollowRequestEntry
	var lastEdge uuid.UUID
	for ; iter.Valid(); iter.Next() {
		if len(requests) >= limit {
			break
		}
		requester, err := uuid.FromBytes(iter.Key()[len(prefix):])
		if err != nil {
			continue // Skip malformed key
		}
		lastEdge = requester
		requests = append(requests, FollowRequestEntry{
			Requester:   requester,
			RequestedAt: string(iter.Value()),
		})
	}
	if err := iter.Error(); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if iter.Valid() && lastEdge != uuid.Nil {
		nextCursor = lastEdge.String()
	}
	return requests, nextCursor, nil
}

// StageDeleteFollowRequestsByTarget removes every pending request against
// target. It runs inside Profile/Group soft delete so a deleted feed never
// leaves approvable requests behind.
func StageDeleteFollowRequestsByTarget(db *store.Store, batch *pebble.Batch, target uuid.UUID) error {
	if batch == nil || target == uuid.Nil {
		return errors.New("batch and target feed UUID are required")
	}
	prefix := NewKeyFrom(FollowRequest.Prefix, target.Bytes())
	_, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
		return batch.Delete(key, nil)
	})
	return err
}

// StageDeleteFollowRequestsByRequester removes every pending request filed
// by requester, across all targets. Request keys are target-first, so this
// streams the whole (small, workflow-only) table; it runs inside the account
// soft-delete critical section.
func StageDeleteFollowRequestsByRequester(db *store.Store, batch *pebble.Batch, requester uuid.UUID) error {
	if batch == nil || requester == uuid.Nil {
		return errors.New("batch and requester UUID are required")
	}
	return FollowRequest.Iter(db, func(key, _ []byte) error {
		suffix := key[len(FollowRequest.Prefix):]
		if len(suffix) != 2*uuid.Size {
			return nil
		}
		keyRequester, err := uuid.FromBytes(suffix[uuid.Size:])
		if err != nil || keyRequester != requester {
			return nil
		}
		return batch.Delete(key, nil)
	})
}
