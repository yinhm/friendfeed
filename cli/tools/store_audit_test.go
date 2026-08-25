package main

import (
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestAuditStoreFindsIndexAndGraphDrift(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	author := uuid.Must(uuid.NewV4())
	follower := uuid.Must(uuid.NewV4())
	require.NoError(t, model.TouchTimelineState(db, author, time.Now().UTC()))
	date := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	for range 2 {
		entryID := uuid.Must(uuid.NewV4())
		_, err := model.PutEntry(db, &pb.Entry{
			Id: entryID.String(), Date: date, ProfileUuid: author.String(),
			From: &pb.Feed{Uuid: author.String(), Id: "author"},
		})
		require.NoError(t, err)
	}

	followKey := model.NewKeyFrom(model.Follow.Prefix, follower.Bytes(), author.Bytes())
	followerKey := model.NewKeyFrom(model.Follower.Prefix, author.Bytes(), follower.Bytes())
	require.NoError(t, db.Put(followKey, []byte("1")))
	require.NoError(t, db.Put(followerKey, []byte("1")))

	orphanOwner := uuid.Must(uuid.NewV4())
	orphanEntry := uuid.Must(uuid.NewV4())
	orphanIndex, err := model.EntryIndexKey(orphanOwner, orphanEntry, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, db.Put(orphanIndex, nil))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 2, stats.entries)
	require.Equal(t, 3, stats.entryIndexes)
	require.Zero(t, stats.missingDirectIndexes)
	require.Equal(t, 1, stats.orphanIndexes)
	require.Equal(t, 2, stats.timelineIndexes)
	require.Equal(t, 2, stats.timelinePositions)
	require.Zero(t, stats.timelineMissingEntry)
	require.Equal(t, 1, stats.sameSecondGroups)
	require.Equal(t, 2, stats.sameSecondEntries)
	require.Equal(t, 1, stats.followEdges)
	require.Equal(t, 1, stats.followerEdges)
	require.Zero(t, stats.missingFollowerEdges)
	require.Zero(t, stats.missingFollowEdges)
	require.Equal(t, 1, stats.maxFollowers)
}

func TestAuditStoreChecksNotificationInvariants(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	recipient := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{
		Uuid: recipient.String(), Id: "notification-recipient", Type: "user",
	}))
	activity := time.Now().UTC().Truncate(time.Millisecond)
	notificationID, err := model.NotificationID(model.NotificationEntryLiked, "audit-like", recipient)
	require.NoError(t, err)
	record := model.NotificationRecord{
		Version: model.NotificationRecordVersion, ID: notificationID.String(),
		Kind: model.NotificationEntryLiked, RecipientUUID: recipient.String(),
		ActivityAtMS: activity.UnixMilli(), CreatedAtNS: activity.UnixNano(),
	}
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		_, _, err := model.StageNotification(db, batch, record)
		return err
	}))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.notifications)
	require.Equal(t, 1, stats.notificationInboxes)
	require.Equal(t, 1, stats.notificationStates)
	require.Zero(t, stats.invalidNotifications)
	require.Zero(t, stats.missingNotificationInbox)
	require.Zero(t, stats.orphanNotificationInbox)
	require.Zero(t, stats.notificationStateMismatch)

	inboxKey, err := model.NotificationInboxKey(recipient, notificationID, activity)
	require.NoError(t, err)
	require.NoError(t, db.Delete(inboxKey))
	orphanID := uuid.Must(uuid.NewV4())
	orphanKey, err := model.NotificationInboxKey(recipient, orphanID, activity.Add(time.Millisecond))
	require.NoError(t, err)
	require.NoError(t, db.Put(orphanKey, nil))
	badState, err := json.Marshal(model.NotificationStateRecord{
		Version: model.NotificationStateVersion, TotalCount: 9, UnreadCount: 9,
	})
	require.NoError(t, err)
	require.NoError(t, db.Put(model.NotificationStateKey(recipient), badState))

	stats, err = auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.missingNotificationInbox)
	require.Equal(t, 1, stats.orphanNotificationInbox)
	require.Equal(t, 1, stats.notificationStateMismatch)
}

func TestAuditStoreChecksServiceRelationships(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	target := uuid.Must(uuid.NewV4())
	actor := uuid.Must(uuid.NewV4())
	var binding *pb.FeedService
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		var stageErr error
		binding, _, stageErr = model.StageAddWebFeedService(db, batch, target, actor, "https://example.com/feed", time.Now().UTC())
		return stageErr
	}))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.services)
	require.Equal(t, 1, stats.serviceStates)
	require.Equal(t, 1, stats.feedServices)
	require.Equal(t, 1, stats.serviceFeedIndexes)
	require.Zero(t, stats.dormantServices)
	require.Zero(t, stats.bindingMissingIndex)

	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	indexKey, err := model.ServiceFeedIndexKey(serviceID, target, binding.Id)
	require.NoError(t, err)
	require.NoError(t, db.Delete(indexKey))
	stats, err = auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.dormantServices)
	require.Equal(t, 1, stats.bindingMissingIndex)
}

func TestAuditStoreChecksGroupInvariants(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	creator := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{
		Uuid: creator.String(), Id: "group-auditor", Name: "Group Auditor", Type: "user",
	}))
	groupProfile, err := model.CreateGroup(db, creator, "audit-group", "Audit Group", "", "", false, time.Now().UTC())
	require.NoError(t, err)
	group := uuid.Must(uuid.FromString(groupProfile.Uuid))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.groups)
	require.Equal(t, 1, stats.groupAdmins)
	require.Zero(t, stats.invalidGroupAdmins)
	require.Zero(t, stats.adminMissingMember)
	require.Zero(t, stats.groupsWithoutAdmins)

	require.NoError(t, db.Delete(model.NewKeyFrom(model.Follow.Prefix, creator.Bytes(), group.Bytes())))
	stats, err = auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.adminMissingMember)

	require.NoError(t, db.Set(model.NewKeyFrom(model.Follow.Prefix, creator.Bytes(), group.Bytes()), []byte("1")))
	adminKey, err := model.GroupAdminKey(group, creator)
	require.NoError(t, err)
	require.NoError(t, db.Delete(adminKey))
	stats, err = auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.groupsWithoutAdmins)

	require.NoError(t, db.Set(adminKey, nil))
	require.NoError(t, model.DeleteGroup(db, group))
	stats, err = auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.deletedGroupResiduals)
	require.Equal(t, 1, stats.invalidGroupAdmins)
}

func TestAuditStoreFindsOrphanMembershipAndFollowRequests(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	requester := uuid.Must(uuid.NewV4())
	publicTarget := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{
		Uuid: requester.String(), Id: "requester", Type: "user",
	}))
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{
		Uuid: publicTarget.String(), Id: "public-target", Type: "user",
	}))

	missingTarget := uuid.Must(uuid.NewV4())
	require.NoError(t, db.Set(model.NewKeyFrom(model.Follow.Prefix, requester.Bytes(), missingTarget.Bytes()), []byte("1")))
	require.NoError(t, db.Set(model.NewKeyFrom(model.Follower.Prefix, missingTarget.Bytes(), requester.Bytes()), []byte("1")))
	deletedGroup := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{
		Uuid: deletedGroup.String(), Id: "deleted-group", Type: "group", Deleted: true,
	}))
	require.NoError(t, db.Set(model.NewKeyFrom(model.Follow.Prefix, requester.Bytes(), deletedGroup.Bytes()), []byte("1")))
	require.NoError(t, db.Set(model.NewKeyFrom(model.Follower.Prefix, deletedGroup.Bytes(), requester.Bytes()), []byte("1")))

	publicRequest, err := model.FollowRequestKey(publicTarget, requester)
	require.NoError(t, err)
	require.NoError(t, db.Set(publicRequest, []byte(time.Now().UTC().Format(time.RFC3339Nano))))
	missingRequestTarget := uuid.Must(uuid.NewV4())
	missingRequest, err := model.FollowRequestKey(missingRequestTarget, requester)
	require.NoError(t, err)
	require.NoError(t, db.Set(missingRequest, []byte(time.Now().UTC().Format(time.RFC3339Nano))))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Zero(t, stats.missingFollowerEdges)
	require.Zero(t, stats.missingFollowEdges)
	require.Equal(t, 2, stats.orphanMemberships)
	require.Equal(t, 2, stats.followRequests)
	require.Equal(t, 2, stats.invalidFollowRequests)
}

func TestAuditStoreReportsLegacyEntryIndexKey(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	author := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	date := time.Now().UTC().Truncate(time.Second)
	entryKey, err := model.PutEntry(db, &pb.Entry{
		Id: entryID.String(), Date: date.Format(time.RFC3339), ProfileUuid: author.String(),
		From: &pb.Feed{Uuid: author.String()},
	})
	require.NoError(t, err)
	// Replace the current-format row with a legacy Flake-shaped row.
	require.NoError(t, model.EntryIndex.RemoveIndex(db, author, date, entryKey))
	legacyKey := store.NewUUIDFlakeKey(model.TableEntryIndex, author, db.TimeTravelReverseId(date)).Bytes()
	require.NoError(t, db.Put(legacyKey, entryKey))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.entryIndexes)
	require.Equal(t, 1, stats.noncanonicalIndexes)
	require.Zero(t, stats.orphanIndexes)
	require.Equal(t, 1, stats.missingDirectIndexes)
}

func TestAuditStoreReportsNoncanonicalEntryKey(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	entryID := uuid.Must(uuid.NewV4())
	putLegacyStringKeyEntry(t, db, &pb.Entry{
		Id: entryID.String(), Date: time.Now().UTC().Format(time.RFC3339),
		ProfileUuid: uuid.Must(uuid.NewV4()).String(),
	})
	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.noncanonicalEntries)
	require.Zero(t, stats.entryKeyIDMismatches)
}

func TestAuditStoreFindsTimelineDrift(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	viewer := uuid.Must(uuid.NewV4())
	base := time.Now().UTC().Truncate(time.Millisecond)
	missingPosition := uuid.Must(uuid.NewV4())
	missingIndex := uuid.Must(uuid.NewV4())
	mismatch := uuid.Must(uuid.NewV4())
	missingEntry := uuid.Must(uuid.NewV4())
	duplicate := uuid.Must(uuid.NewV4())
	for _, entry := range []uuid.UUID{missingPosition, missingIndex, mismatch, duplicate} {
		_, err := model.Entry.Put(db, entry.Bytes(), &pb.Entry{Id: entry.String(), Date: base.Format(time.RFC3339), ProfileUuid: viewer.String()})
		require.NoError(t, err)
	}
	for _, entry := range []uuid.UUID{missingPosition, missingIndex, mismatch, missingEntry, duplicate} {
		_, err := model.MoveTimelineEntry(db, viewer, entry, base, nil)
		require.NoError(t, err)
	}
	require.NoError(t, db.Delete(model.TimelinePositionKey(viewer, missingPosition)))
	missingIndexKey, err := model.TimelineIndexKey(viewer, missingIndex, base)
	require.NoError(t, err)
	require.NoError(t, db.Delete(missingIndexKey))
	var wrong [8]byte
	binary.BigEndian.PutUint64(wrong[:], uint64(base.Add(time.Minute).UnixMilli()))
	require.NoError(t, db.Put(model.TimelinePositionKey(viewer, mismatch), wrong[:]))
	duplicateKey, err := model.TimelineIndexKey(viewer, duplicate, base.Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, db.Put(duplicateKey, nil))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.timelineMissingEntry)
	require.Equal(t, 1, stats.timelineMissingPos)
	require.Equal(t, 1, stats.timelineMissingIndex)
	require.Equal(t, 1, stats.timelineDuplicates)
	require.Equal(t, 1, stats.timelineTimeMismatch)
}
