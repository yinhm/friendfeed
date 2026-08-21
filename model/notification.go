package model

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/store"
)

const (
	NotificationRecordVersion = 1
	NotificationStateVersion  = 1

	NotificationMaxEntries  = 500
	NotificationTrimTrigger = 550
	NotificationTrimBatch   = 100

	NotificationInboxPositionSize = 8 + uuid.Size
)

type NotificationKind string

const (
	NotificationFollowRequestReceived NotificationKind = "FOLLOW_REQUEST_RECEIVED"
	NotificationFollowRequestApproved NotificationKind = "FOLLOW_REQUEST_APPROVED"
	NotificationFollowRequestRejected NotificationKind = "FOLLOW_REQUEST_REJECTED"
	NotificationEntryCommented        NotificationKind = "ENTRY_COMMENTED"
	NotificationEntryLiked            NotificationKind = "ENTRY_LIKED"
	NotificationGroupAdminAdded       NotificationKind = "GROUP_ADMIN_ADDED"
	NotificationGroupAdminRemoved     NotificationKind = "GROUP_ADMIN_REMOVED"
	NotificationGroupMemberRemoved    NotificationKind = "GROUP_MEMBER_REMOVED"
)

type NotificationRecord struct {
	Version            uint32           `json:"version"`
	ID                 string           `json:"id"`
	Kind               NotificationKind `json:"kind"`
	RecipientUUID      string           `json:"recipient_uuid"`
	ActorUUID          string           `json:"actor_uuid,omitempty"`
	TargetUUID         string           `json:"target_uuid,omitempty"`
	EntryUUID          string           `json:"entry_uuid,omitempty"`
	CommentUUID        string           `json:"comment_uuid,omitempty"`
	RequestedAt        string           `json:"requested_at,omitempty"`
	ActivityAtMS       int64            `json:"activity_at_ms"`
	CreatedAtNS        int64            `json:"created_at_ns"`
	ActorNameSnapshot  string           `json:"actor_name_snapshot,omitempty"`
	TargetNameSnapshot string           `json:"target_name_snapshot,omitempty"`
}

type NotificationStateRecord struct {
	Version      uint32 `json:"version"`
	LastReadAtNS int64  `json:"last_read_at_ns"`
	UnreadCount  uint32 `json:"unread_count"`
	TotalCount   uint32 `json:"total_count"`
}

func NotificationID(kind NotificationKind, occurrence string, recipient uuid.UUID) (uuid.UUID, error) {
	if kind == "" || occurrence == "" || recipient == uuid.Nil {
		return uuid.Nil, errors.New("notification kind, occurrence and recipient are required")
	}
	return uuid.NewV5(uuid.NamespaceURL, fmt.Sprintf("notification:%s:%s:%s", kind, occurrence, recipient)), nil
}

func NotificationKey(recipient, notificationID uuid.UUID) store.Key {
	return NewKeyFrom(Notification.Prefix, recipient.Bytes(), notificationID.Bytes())
}

func NotificationPrefix(recipient uuid.UUID) store.Key {
	return NewKeyFrom(Notification.Prefix, recipient.Bytes())
}

func NotificationInboxPrefix(recipient uuid.UUID) store.Key {
	return NewKeyFrom(NotificationInbox.Prefix, recipient.Bytes())
}

func NotificationStateKey(recipient uuid.UUID) store.Key {
	return NewKeyFrom(NotificationState.Prefix, recipient.Bytes())
}

func NotificationInboxKey(recipient, notificationID uuid.UUID, activity time.Time) (store.Key, error) {
	reverse, err := reverseTimelineMillis(activity)
	if err != nil {
		return nil, err
	}
	return NewKeyFrom(NotificationInbox.Prefix, recipient.Bytes(), reverse[:], notificationID.Bytes()), nil
}

func ParseNotificationInboxKey(key store.Key) (recipient, notificationID uuid.UUID, activity time.Time, err error) {
	const size = 4 + uuid.Size + NotificationInboxPositionSize
	if len(key) != size {
		return recipient, notificationID, activity, fmt.Errorf("invalid NotificationInbox key length %d", len(key))
	}
	recipient, err = uuid.FromBytes(key[4 : 4+uuid.Size])
	if err != nil {
		return recipient, notificationID, activity, err
	}
	ms := ^binary.BigEndian.Uint64(key[4+uuid.Size : 4+uuid.Size+8])
	if ms > uint64(^uint64(0)>>1) {
		return recipient, notificationID, activity, errors.New("notification timestamp overflows int64")
	}
	notificationID, err = uuid.FromBytes(key[4+uuid.Size+8:])
	return recipient, notificationID, time.UnixMilli(int64(ms)).UTC(), err
}

func NotificationInboxPosition(key store.Key, recipient uuid.UUID) ([]byte, error) {
	prefix := NotificationInboxPrefix(recipient)
	if !bytes.HasPrefix(key, prefix) || len(key)-len(prefix) != NotificationInboxPositionSize {
		return nil, errors.New("invalid notification inbox key")
	}
	return append([]byte(nil), key[len(prefix):]...), nil
}

func loadNotificationState(db *store.Store, recipient uuid.UUID) (NotificationStateRecord, error) {
	state := NotificationStateRecord{Version: NotificationStateVersion}
	raw, err := db.Get(NotificationStateKey(recipient))
	if errors.Is(err, store.ErrNotFound) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, fmt.Errorf("decode notification state: %w", err)
	}
	if state.Version != NotificationStateVersion {
		return state, fmt.Errorf("unsupported notification state version %d", state.Version)
	}
	return state, nil
}

func GetNotificationState(db *store.Store, recipient uuid.UUID) (NotificationStateRecord, error) {
	if recipient == uuid.Nil {
		return NotificationStateRecord{}, errors.New("notification recipient is required")
	}
	return loadNotificationState(db, recipient)
}

func encodeNotificationState(state NotificationStateRecord) ([]byte, error) {
	state.Version = NotificationStateVersion
	return json.Marshal(state)
}

func GetNotification(db *store.Store, recipient, notificationID uuid.UUID) (NotificationRecord, error) {
	var record NotificationRecord
	raw, err := db.Get(NotificationKey(recipient, notificationID))
	if errors.Is(err, store.ErrNotFound) {
		return record, ErrNotFound
	}
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return record, fmt.Errorf("decode notification: %w", err)
	}
	if record.Version != NotificationRecordVersion {
		return record, fmt.Errorf("unsupported notification version %d", record.Version)
	}
	return record, nil
}

// StageNotification adds one recipient-owned notification to the caller's
// atomic batch. Deterministic IDs make it idempotent: an already-existing
// canonical row is a no-op and does not increment State counters again.
// Missing/deleted/non-user recipients are also a no-op: notification delivery
// must never make an otherwise-valid domain mutation fail just because its
// recipient disappeared concurrently.
func StageNotification(db *store.Store, batch *pebble.Batch, record NotificationRecord) (created, needsTrim bool, err error) {
	if batch == nil {
		return false, false, errors.New("notification batch is required")
	}
	recipient, err := uuid.FromString(record.RecipientUUID)
	if err != nil || recipient == uuid.Nil {
		return false, false, errors.New("valid notification recipient is required")
	}
	notificationID, err := uuid.FromString(record.ID)
	if err != nil || notificationID == uuid.Nil {
		return false, false, errors.New("valid notification id is required")
	}
	profile, err := GetProfileFromUuid(db, recipient)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrProfileDeleted) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if profile.Type != "user" {
		return false, false, nil
	}
	if record.ActorUUID != "" && record.ActorUUID == record.RecipientUUID {
		return false, false, nil
	}

	canonicalKey := NotificationKey(recipient, notificationID)
	exists, err := db.Exists(canonicalKey)
	if err != nil {
		return false, false, err
	}
	if exists {
		return false, false, nil
	}
	if record.Kind == "" || record.ActivityAtMS < 0 || record.CreatedAtNS <= 0 {
		return false, false, errors.New("notification kind and timestamps are required")
	}
	record.Version = NotificationRecordVersion
	encoded, err := json.Marshal(record)
	if err != nil {
		return false, false, err
	}
	inboxKey, err := NotificationInboxKey(recipient, notificationID, time.UnixMilli(record.ActivityAtMS))
	if err != nil {
		return false, false, err
	}
	state, err := loadNotificationState(db, recipient)
	if err != nil {
		return false, false, err
	}
	state.TotalCount++
	if record.CreatedAtNS > state.LastReadAtNS {
		state.UnreadCount++
	}
	encodedState, err := encodeNotificationState(state)
	if err != nil {
		return false, false, err
	}
	if err := batch.Set(canonicalKey, encoded, nil); err != nil {
		return false, false, err
	}
	if err := batch.Set(inboxKey, nil, nil); err != nil {
		return false, false, err
	}
	if err := batch.Set(NotificationStateKey(recipient), encodedState, nil); err != nil {
		return false, false, err
	}
	return true, state.TotalCount > NotificationTrimTrigger, nil
}

func ListNotifications(db *store.Store, recipient uuid.UUID, limit int, cursor []byte) ([]NotificationRecord, []byte, error) {
	if recipient == uuid.Nil {
		return nil, nil, errors.New("notification recipient is required")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	if len(cursor) != 0 && len(cursor) != NotificationInboxPositionSize {
		return nil, nil, errors.New("invalid notification cursor")
	}
	prefix := NotificationInboxPrefix(recipient)
	iter, err := db.NewIterator(prefix)
	if err != nil {
		return nil, nil, err
	}
	defer iter.Close()
	if len(cursor) != 0 {
		cursorKey := NewKeyFrom(prefix, cursor)
		iter.SeekGE(cursorKey)
		if iter.Valid() && bytes.Equal(iter.UnsafeRawKey(), cursorKey) {
			iter.Next()
		}
	} else {
		iter.First()
	}

	records := make([]NotificationRecord, 0, limit+1)
	keys := make([]store.Key, 0, limit+1)
	for iter.Valid() && len(records) <= limit {
		indexKey := iter.Key()
		_, notificationID, _, parseErr := ParseNotificationInboxKey(indexKey)
		if parseErr != nil {
			iter.Next()
			continue
		}
		record, getErr := GetNotification(db, recipient, notificationID)
		if errors.Is(getErr, ErrNotFound) {
			_ = db.Delete(indexKey)
			iter.Next()
			continue
		}
		if getErr != nil {
			return nil, nil, getErr
		}
		records = append(records, record)
		keys = append(keys, indexKey)
		iter.Next()
	}
	if err := iter.Error(); err != nil {
		return nil, nil, err
	}
	if len(records) <= limit {
		return records, nil, nil
	}
	next, err := NotificationInboxPosition(keys[limit-1], recipient)
	if err != nil {
		return nil, nil, err
	}
	return records[:limit], next, nil
}

func MarkNotificationsRead(db *store.Store, recipient uuid.UUID, at time.Time) error {
	if recipient == uuid.Nil {
		return errors.New("notification recipient is required")
	}
	atNS := at.UTC().UnixNano()
	if atNS <= 0 {
		return errors.New("notification read time must be after Unix epoch")
	}
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		state, err := loadNotificationState(db, recipient)
		if err != nil {
			return err
		}
		if atNS <= state.LastReadAtNS {
			return nil
		}
		state.LastReadAtNS = atNS
		state.UnreadCount = 0
		encoded, err := encodeNotificationState(state)
		if err != nil {
			return err
		}
		return batch.Set(NotificationStateKey(recipient), encoded, nil)
	})
}

// TrimNotifications removes old recipient-owned rows in bounded batches.
// It keeps the newest NotificationMaxEntries Inbox rows by activity order.
func TrimNotifications(db *store.Store, recipient uuid.UUID, maxDelete int) (trimmed int, remaining bool, err error) {
	if recipient == uuid.Nil {
		return 0, false, errors.New("notification recipient is required")
	}
	if maxDelete <= 0 || maxDelete > NotificationTrimBatch {
		maxDelete = NotificationTrimBatch
	}
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		prefix := NotificationInboxPrefix(recipient)
		iter, err := db.NewIterator(prefix)
		if err != nil {
			return err
		}
		defer iter.Close()
		state, err := loadNotificationState(db, recipient)
		if err != nil {
			return err
		}
		seen := 0
		for iter.First(); iter.Valid(); iter.Next() {
			seen++
			if seen <= NotificationMaxEntries {
				continue
			}
			if trimmed >= maxDelete {
				remaining = true
				break
			}
			indexKey := iter.Key()
			_, notificationID, _, parseErr := ParseNotificationInboxKey(indexKey)
			if parseErr != nil {
				if err := batch.Delete(indexKey, nil); err != nil {
					return err
				}
				trimmed++
				continue
			}
			record, getErr := GetNotification(db, recipient, notificationID)
			canonicalExists := getErr == nil
			if getErr != nil && !errors.Is(getErr, ErrNotFound) {
				return getErr
			}
			if err := batch.Delete(indexKey, nil); err != nil {
				return err
			}
			if canonicalExists {
				if err := batch.Delete(NotificationKey(recipient, notificationID), nil); err != nil {
					return err
				}
				if state.TotalCount > 0 {
					state.TotalCount--
				}
				if record.CreatedAtNS > state.LastReadAtNS && state.UnreadCount > 0 {
					state.UnreadCount--
				}
			}
			trimmed++
		}
		if err := iter.Error(); err != nil {
			return err
		}
		encoded, err := encodeNotificationState(state)
		if err != nil {
			return err
		}
		return batch.Set(NotificationStateKey(recipient), encoded, nil)
	})
	return trimmed, remaining, err
}
