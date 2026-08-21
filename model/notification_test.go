package model

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func notificationTestUser(t *testing.T, db *store.Store, name string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{
		Uuid: id.String(),
		Id:   name,
		Name: name,
		Type: "user",
	}))
	return id
}

func notificationTestRecord(t *testing.T, kind NotificationKind, recipient uuid.UUID, occurrence string, activity, created time.Time) NotificationRecord {
	t.Helper()
	id, err := NotificationID(kind, occurrence, recipient)
	require.NoError(t, err)
	return NotificationRecord{
		ID:            id.String(),
		Kind:          kind,
		RecipientUUID: recipient.String(),
		ActivityAtMS:  activity.UnixMilli(),
		CreatedAtNS:   created.UnixNano(),
	}
}

func stageNotificationTestRecord(t *testing.T, db *store.Store, record NotificationRecord) (bool, bool) {
	t.Helper()
	var created, needsTrim bool
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		var err error
		created, needsTrim, err = StageNotification(db, batch, record)
		return err
	}))
	return created, needsTrim
}

func TestNotificationInboxKeyUsesReverseMillisAndUUIDTiebreak(t *testing.T) {
	recipient := uuid.Must(uuid.NewV4())
	first := uuid.Must(uuid.NewV4())
	second := uuid.Must(uuid.NewV4())
	at := time.Date(2026, 8, 21, 10, 0, 0, 123000000, time.UTC)

	firstKey, err := NotificationInboxKey(recipient, first, at)
	require.NoError(t, err)
	secondKey, err := NotificationInboxKey(recipient, second, at)
	require.NoError(t, err)

	gotRecipient, gotID, gotAt, err := ParseNotificationInboxKey(firstKey)
	require.NoError(t, err)
	require.Equal(t, recipient, gotRecipient)
	require.Equal(t, first, gotID)
	require.Equal(t, at.Truncate(time.Millisecond), gotAt)

	keys := [][]byte{firstKey, secondKey}
	sort.Slice(keys, func(i, j int) bool { return string(keys[i]) < string(keys[j]) })
	_, lowerID, _, err := ParseNotificationInboxKey(keys[0])
	require.NoError(t, err)
	_, upperID, _, err := ParseNotificationInboxKey(keys[1])
	require.NoError(t, err)
	require.Less(t, string(lowerID.Bytes()), string(upperID.Bytes()))
}

func TestStageNotificationIsIdempotentAndMarkReadUsesCreatedAt(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	recipient := notificationTestUser(t, db, "recipient")
	activity := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 8, 21, 9, 0, 0, 100, time.UTC)
	record := notificationTestRecord(t, NotificationEntryLiked, recipient, "entry:actor", activity, createdAt)

	created, needsTrim := stageNotificationTestRecord(t, db, record)
	require.True(t, created)
	require.False(t, needsTrim)
	created, _ = stageNotificationTestRecord(t, db, record)
	require.False(t, created)

	state, err := GetNotificationState(db, recipient)
	require.NoError(t, err)
	require.Equal(t, uint32(1), state.TotalCount)
	require.Equal(t, uint32(1), state.UnreadCount)

	require.NoError(t, MarkNotificationsRead(db, recipient, createdAt.Add(time.Second)))
	state, err = GetNotificationState(db, recipient)
	require.NoError(t, err)
	require.Equal(t, uint32(0), state.UnreadCount)

	// The second notification has an older activity_at (so it sorts into the
	// past) but is written after mark-read. created_at, not activity_at, makes
	// it unread.
	laterWrite := createdAt.Add(2 * time.Second)
	oldActivity := activity.Add(-24 * time.Hour)
	second := notificationTestRecord(t, NotificationEntryCommented, recipient, "old-comment", oldActivity, laterWrite)
	created, _ = stageNotificationTestRecord(t, db, second)
	require.True(t, created)
	state, err = GetNotificationState(db, recipient)
	require.NoError(t, err)
	require.Equal(t, uint32(1), state.UnreadCount)

	items, _, err := ListNotifications(db, recipient, 10, nil)
	require.NoError(t, err)
	require.Len(t, items, 2)
	// Inbox ordering stays by domain activity, not notification write time.
	require.Equal(t, record.ID, items[0].ID)
	require.Equal(t, second.ID, items[1].ID)
}

func TestStageNotificationSkipsNonUserAndSelfRecipient(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	user := notificationTestUser(t, db, "user")
	group := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: group.String(), Id: "group", Name: "Group", Type: "group"}))
	now := time.Now().UTC()

	groupRecord := notificationTestRecord(t, NotificationEntryLiked, group, "group-like", now, now)
	created, _ := stageNotificationTestRecord(t, db, groupRecord)
	require.False(t, created)

	self := notificationTestRecord(t, NotificationEntryLiked, user, "self-like", now, now)
	self.ActorUUID = user.String()
	created, _ = stageNotificationTestRecord(t, db, self)
	require.False(t, created)
}

func TestTrimNotificationsKeepsNewestFiveHundred(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	recipient := notificationTestUser(t, db, "recipient")
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < NotificationTrimTrigger+1; i++ {
		at := base.Add(time.Duration(i) * time.Millisecond)
		record := notificationTestRecord(t, NotificationEntryLiked, recipient, fmt.Sprintf("like-%d", i), at, at.Add(time.Second))
		created, _ := stageNotificationTestRecord(t, db, record)
		require.True(t, created)
	}

	state, err := GetNotificationState(db, recipient)
	require.NoError(t, err)
	require.Equal(t, uint32(NotificationTrimTrigger+1), state.TotalCount)

	for {
		_, remaining, err := TrimNotifications(db, recipient, NotificationTrimBatch)
		require.NoError(t, err)
		if !remaining {
			break
		}
	}
	state, err = GetNotificationState(db, recipient)
	require.NoError(t, err)
	require.Equal(t, uint32(NotificationMaxEntries), state.TotalCount)
	require.Equal(t, uint32(NotificationMaxEntries), state.UnreadCount)

	items, next, err := ListNotifications(db, recipient, 100, nil)
	require.NoError(t, err)
	require.Len(t, items, 100)
	require.Len(t, next, NotificationInboxPositionSize)
	// Newest retained occurrence was the final inserted row.
	expected, err := NotificationID(NotificationEntryLiked, fmt.Sprintf("like-%d", NotificationTrimTrigger), recipient)
	require.NoError(t, err)
	require.Equal(t, expected.String(), items[0].ID)
}
