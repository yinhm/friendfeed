package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

const (
	GroupActivityCreateScore  int64 = 100
	GroupActivityPostScore    int64 = 10
	GroupActivityLikeScore    int64 = 3
	GroupActivityCommentScore int64 = 4
)

// GroupActivity is the rebuildable, sorted sidebar ranking for one user.
// It is derived state: Follow defines which Groups are eligible and the
// current Entry/Like/Comment tables define the score.
type GroupActivity struct {
	GroupUUID string `json:"group_uuid"`
	Score     int64  `json:"score"`
}

var groupActivityMetaPrefix = []byte("group-activity/v1/")
var groupOwnerMetaPrefix = []byte("group-owner/v1/")

func GroupActivityMetaKey(user uuid.UUID) store.Key {
	return NewKeyFrom(TableMeta.Bytes(), groupActivityMetaPrefix, user.Bytes())
}

func groupOwnerMetaKey(group uuid.UUID) store.Key {
	return NewKeyFrom(TableMeta.Bytes(), groupOwnerMetaPrefix, group.Bytes())
}

func sortGroupActivity(rows []GroupActivity) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].GroupUUID < rows[j].GroupUUID
	})
}

func GetGroupActivity(db *store.Store, user uuid.UUID) ([]GroupActivity, error) {
	if user == uuid.Nil {
		return nil, errors.New("user UUID is required")
	}
	raw, err := db.Get(GroupActivityMetaKey(user))
	if err != nil {
		return nil, err
	}
	var rows []GroupActivity
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode Group activity for %s: %w", user, err)
	}
	return rows, nil
}

func stageWriteGroupActivity(batch *pebble.Batch, user uuid.UUID, rows []GroupActivity) error {
	sortGroupActivity(rows)
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return batch.Set(GroupActivityMetaKey(user), raw, nil)
}

// StageAdjustGroupActivity applies one score delta to the user's materialized
// ranking. Callers must already be inside Store.ApplyBatch.
func StageAdjustGroupActivity(db *store.Store, batch *pebble.Batch, user, group uuid.UUID, delta int64) error {
	if batch == nil || user == uuid.Nil || group == uuid.Nil {
		return errors.New("batch, user UUID, and group UUID are required")
	}
	rows, err := GetGroupActivity(db, user)
	if errors.Is(err, store.ErrNotFound) {
		rows = nil
	} else if err != nil {
		return err
	}
	for i := range rows {
		if rows[i].GroupUUID != group.String() {
			continue
		}
		rows[i].Score += delta
		if rows[i].Score < 0 {
			rows[i].Score = 0
		}
		return stageWriteGroupActivity(batch, user, rows)
	}
	if delta < 0 {
		delta = 0
	}
	rows = append(rows, GroupActivity{GroupUUID: group.String(), Score: delta})
	return stageWriteGroupActivity(batch, user, rows)
}

func stageAdjustGroupActivityIfMember(db *store.Store, batch *pebble.Batch, user, group uuid.UUID, delta int64) error {
	member, err := IsGroupMember(db, group, user)
	if err != nil {
		return err
	}
	if !member {
		// Leave hides the row through the authoritative Follow edge but retains
		// its score for a future rejoin. Only deletions of already-counted facts
		// remain applicable while the user is away; new activity does not count.
		if delta > 0 {
			return nil
		}
		rows, err := GetGroupActivity(db, user)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		tracked := false
		for _, row := range rows {
			if row.GroupUUID == group.String() {
				tracked = true
				break
			}
		}
		if !tracked {
			return nil
		}
	}
	profile, err := getGroupProfile(db, group)
	if err != nil {
		return err
	}
	if profile.Deleted {
		return nil
	}
	return StageAdjustGroupActivity(db, batch, user, group, delta)
}

func entryGroupUUID(db *store.Store, entry *pb.Entry) (uuid.UUID, bool, error) {
	group, err := uuid.FromString(entry.FeedUuid)
	if err != nil || group == uuid.Nil {
		return uuid.Nil, false, nil
	}
	profile := new(pb.Profile)
	if err := Profile.Get(db, group.Bytes(), profile); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrProfileDeleted) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return group, profile.Type == "group" && !profile.Deleted, nil
}

// RebuildGroupActivityForUser derives one user's ranking from current
// membership and authoritative interaction rows, using bounded prefix scans.
func RebuildGroupActivityForUser(db *store.Store, user uuid.UUID) ([]GroupActivity, error) {
	groups := make(map[uuid.UUID]int64)
	followPrefix := NewKeyFrom(Follow.Prefix, user.Bytes())
	if _, err := db.ForwardScan(followPrefix, func(_ int, key, _ []byte) error {
		group, err := uuid.FromBytes(key[len(followPrefix):])
		if err != nil {
			return nil
		}
		profile, err := getGroupProfile(db, group)
		if err == nil && !profile.Deleted {
			groups[group] = 0
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		// No Group membership: the ranking is empty by definition. Skip the
		// EntryIndex/LikeTimeline/CommentTimeline scans, which otherwise walk
		// the user's entire history for nothing.
		return nil, nil
	}
	for group := range groups {
		owner, err := db.Get(groupOwnerMetaKey(group))
		if err == nil {
			if ownerUUID, parseErr := uuid.FromBytes(owner); parseErr == nil && ownerUUID == user {
				groups[group] += GroupActivityCreateScore
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}

	entryPrefix := EntryIndex.PrefixAppend(user.Bytes())
	if _, err := db.ForwardScan(entryPrefix, func(_ int, key, _ []byte) error {
		_, entryID, _, err := ParseEntryIndexKey(key)
		if err != nil {
			return err
		}
		entry := new(pb.Entry)
		if err := Entry.Get(db, entryID.Bytes(), entry); err != nil {
			return nil
		}
		if entry.ProfileUuid != user.String() {
			return nil
		}
		group, ok, err := entryGroupUUID(db, entry)
		if err == nil && ok {
			if _, joined := groups[group]; joined {
				groups[group] += GroupActivityPostScore
			}
		}
		return err
	}); err != nil {
		return nil, err
	}

	likePrefix := LikeTimelinePrefix(user)
	if _, err := db.ForwardScan(likePrefix, func(_ int, key, _ []byte) error {
		_, entryID, _, err := ParseInteractionTimelineKey(key, TableLikeTimeline)
		if err != nil {
			return err
		}
		entry := new(pb.Entry)
		if err := Entry.Get(db, entryID.Bytes(), entry); err != nil {
			return nil
		}
		group, ok, err := entryGroupUUID(db, entry)
		if err == nil && ok {
			if _, joined := groups[group]; joined {
				groups[group] += GroupActivityLikeScore
			}
		}
		return err
	}); err != nil {
		return nil, err
	}

	commentPrefix := CommentTimelinePrefix(user)
	if _, err := db.ForwardScan(commentPrefix, func(_ int, key, _ []byte) error {
		_, entryID, _, err := ParseInteractionTimelineKey(key, TableCommentTimeline)
		if err != nil {
			return err
		}
		entry, err := GetEntry(db, entryID.String())
		if err != nil {
			return nil
		}
		group, ok, err := entryGroupUUID(db, entry)
		if err != nil || !ok {
			return err
		}
		if _, joined := groups[group]; !joined {
			return nil
		}
		for _, comment := range entry.Comments {
			if comment != nil && comment.GetFrom().GetUuid() == user.String() {
				groups[group] += GroupActivityCommentScore
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	rows := make([]GroupActivity, 0, len(groups))
	for group, score := range groups {
		rows = append(rows, GroupActivity{GroupUUID: group.String(), Score: score})
	}
	sortGroupActivity(rows)
	return rows, nil
}

func ReplaceGroupActivity(db *store.Store, user uuid.UUID, rows []GroupActivity) error {
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		return stageWriteGroupActivity(batch, user, rows)
	})
}
