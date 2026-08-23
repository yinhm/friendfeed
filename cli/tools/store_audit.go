package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/protobuf/proto"
)

type storeAuditStats struct {
	entries                      int
	noncanonicalEntries          int
	entryKeyIDMismatches         int
	entryIndexes                 int
	noncanonicalIndexes          int
	missingDirectIndexes         int
	orphanIndexes                int
	timelineIndexes              int
	timelinePositions            int
	timelineMissingEntry         int
	timelineMissingPos           int
	timelineMissingIndex         int
	timelineDuplicates           int
	timelineTimeMismatch         int
	timelineViewers              int
	timelineInactiveRows         int
	timelineOverLimit            int
	sameSecondGroups             int
	sameSecondEntries            int
	followEdges                  int
	followerEdges                int
	missingFollowerEdges         int
	missingFollowEdges           int
	maxFollowers                 int
	services                     int
	serviceStates                int
	feedServices                 int
	serviceFeedIndexes           int
	dormantServices              int
	stateMissingService          int
	bindingMissingSource         int
	bindingMissingIndex          int
	disabledWithIndex            int
	orphanServiceIndexes         int
	likeTimelineRows             int
	commentTimelineRows          int
	commentPositionRows          int
	interactionOrphans           int
	interactionMismatches        int
	groups                       int
	groupAdmins                  int
	invalidGroupAdmins           int
	adminMissingMember           int
	groupsWithoutAdmins          int
	deletedGroupResiduals        int
	groupIndexRows               int
	invalidGroupIndexRows        int
	orphanGroupIndexRows         int
	missingGroupIndexRows        int
	duplicateGroupIndexRows      int
	notifications                int
	notificationInboxes          int
	notificationStates           int
	invalidNotifications         int
	orphanNotificationRecipients int
	missingNotificationInbox     int
	orphanNotificationInbox      int
	notificationInboxMismatch    int
	notificationStateMismatch    int
	notificationOverMax          int
	notificationOverTrigger      int
	tasks                        taskqueue.AuditStats
}

func auditStore(db *store.Store) (storeAuditStats, error) {
	stats := storeAuditStats{}
	expectedDirectIndexes := 0
	foundDirectIndexes := 0

	if err := model.Entry.Iter(db, func(key, raw []byte) error {
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode Entry[%x]: %w", key, err)
		}
		author, err := uuid.FromString(entry.ProfileUuid)
		if err != nil {
			return fmt.Errorf("entry %q author UUID: %w", entry.Id, err)
		}
		feed := author
		if entry.FeedUuid != "" {
			feed, err = uuid.FromString(entry.FeedUuid)
			if err != nil {
				return fmt.Errorf("entry %q feed UUID: %w", entry.Id, err)
			}
		}
		date, err := time.Parse(time.RFC3339, entry.Date)
		if err != nil {
			return fmt.Errorf("entry %q date: %w", entry.Id, err)
		}
		entryID, idErr := uuid.FromString(entry.Id)
		if idErr != nil {
			stats.entryKeyIDMismatches++
			return nil
		}
		canonicalKey := model.Entry.PrefixAppend(entryID.Bytes())
		if !bytes.Equal(key, canonicalKey) {
			stats.noncanonicalEntries++
			if len(key) != model.Entry.Prefix.Len()+36 {
				stats.entryKeyIDMismatches++
			} else if keyID, err := uuid.FromString(string(key[model.Entry.Prefix.Len():])); err != nil || keyID != entryID {
				stats.entryKeyIDMismatches++
			}
		}
		owners := []uuid.UUID{author}
		if feed != author {
			owners = append(owners, feed)
		}
		for _, owner := range owners {
			expectedDirectIndexes++
			expected, err := expectedEntryIndexKey(owner, date, canonicalKey)
			if err != nil {
				return err
			}
			exists, err := db.Exists(expected)
			if err != nil {
				return err
			}
			if !exists {
				stats.missingDirectIndexes++
				continue
			}
			foundDirectIndexes++
		}
		stats.entries++
		return nil
	}); err != nil {
		return stats, err
	}

	if err := model.Follow.Iter(db, func(key, _ []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Follow key length %d", len(key))
		}
		follower, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		feed, _ := uuid.FromBytes(key[4+uuid.Size:])
		counterpart := model.NewKeyFrom(model.Follower.Prefix, feed.Bytes(), follower.Bytes())
		exists, err := db.Exists(counterpart)
		if err != nil {
			return err
		}
		if !exists {
			stats.missingFollowerEdges++
		}
		stats.followEdges++
		return nil
	}); err != nil {
		return stats, err
	}
	var followerFeed uuid.UUID
	followerCount := 0
	finishFollowerFeed := func() {
		stats.maxFollowers = max(stats.maxFollowers, followerCount)
	}
	if err := model.Follower.Iter(db, func(key, _ []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Follower key length %d", len(key))
		}
		feed, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		if followerCount > 0 && feed != followerFeed {
			finishFollowerFeed()
			followerCount = 0
		}
		followerFeed = feed
		followerCount++
		stats.followerEdges++
		return nil
	}); err != nil {
		return stats, err
	}
	finishFollowerFeed()
	// Follow keys are unique. The first pass already counted every Follow row
	// whose Follower counterpart exists, so the remaining Follower rows are
	// precisely the reverse-only edges; a second point lookup per row is waste.
	matchedGraphEdges := stats.followEdges - stats.missingFollowerEdges
	stats.missingFollowEdges = stats.followerEdges - matchedGraphEdges
	if stats.missingFollowEdges < 0 {
		return stats, errors.New("Follower table contains duplicate keys")
	}

	if err := model.Service.Iter(db, func(key, _ []byte) error {
		if len(key) != model.Service.Prefix.Len()+uuid.Size {
			return fmt.Errorf("invalid Service key length %d", len(key))
		}
		serviceID, _ := uuid.FromBytes(key[model.Service.Prefix.Len():])
		count, err := db.ForwardScan(model.ServiceFeedIndex.PrefixAppend(serviceID.Bytes()), func(int, []byte, []byte) error { return nil })
		if err != nil {
			return err
		}
		if count == 0 {
			stats.dormantServices++
		}
		stats.services++
		return nil
	}); err != nil {
		return stats, err
	}
	if err := model.ServiceState.Iter(db, func(key, raw []byte) error {
		if len(key) != model.ServiceState.Prefix.Len()+uuid.Size {
			return fmt.Errorf("invalid ServiceState key length %d", len(key))
		}
		state := new(pb.ServiceState)
		if err := proto.Unmarshal(raw, state); err != nil {
			return fmt.Errorf("decode ServiceState[%x]: %w", key, err)
		}
		serviceID, _ := uuid.FromBytes(key[model.ServiceState.Prefix.Len():])
		exists, err := db.Exists(model.Service.PrefixAppend(serviceID.Bytes()))
		if err != nil {
			return err
		}
		if !exists {
			stats.stateMissingService++
		}
		stats.serviceStates++
		return nil
	}); err != nil {
		return stats, err
	}
	if err := model.FeedService.Iter(db, func(key, raw []byte) error {
		if len(key) <= model.FeedService.Prefix.Len()+uuid.Size {
			return fmt.Errorf("invalid FeedService key length %d", len(key))
		}
		target, _ := uuid.FromBytes(key[model.FeedService.Prefix.Len() : model.FeedService.Prefix.Len()+uuid.Size])
		serviceID := string(key[model.FeedService.Prefix.Len()+uuid.Size:])
		binding := new(pb.FeedService)
		if err := proto.Unmarshal(raw, binding); err != nil {
			return fmt.Errorf("decode FeedService[%x]: %w", key, err)
		}
		if binding.ServiceUuid != "" {
			source, err := uuid.FromString(binding.ServiceUuid)
			if err != nil {
				return fmt.Errorf("FeedService %s/%s service UUID: %w", target, serviceID, err)
			}
			exists, err := db.Exists(model.Service.PrefixAppend(source.Bytes()))
			if err != nil {
				return err
			}
			if !exists {
				stats.bindingMissingSource++
			}
			indexKey, err := model.ServiceFeedIndexKey(source, target, serviceID)
			if err != nil {
				return err
			}
			indexed, err := db.Exists(indexKey)
			if err != nil {
				return err
			}
			if binding.Enabled && !indexed {
				stats.bindingMissingIndex++
			} else if !binding.Enabled && indexed {
				stats.disabledWithIndex++
			}
		}
		stats.feedServices++
		return nil
	}); err != nil {
		return stats, err
	}
	if err := model.ServiceFeedIndex.Iter(db, func(key, _ []byte) error {
		const fixed = 2 * uuid.Size
		if len(key) <= model.ServiceFeedIndex.Prefix.Len()+fixed {
			return fmt.Errorf("invalid ServiceFeedIndex key length %d", len(key))
		}
		offset := model.ServiceFeedIndex.Prefix.Len()
		target, _ := uuid.FromBytes(key[offset+uuid.Size : offset+fixed])
		serviceID := string(key[offset+fixed:])
		bindingKey, err := model.FeedServiceKey(target, serviceID)
		if err != nil {
			return err
		}
		exists, err := db.Exists(bindingKey)
		if err != nil {
			return err
		}
		if !exists {
			stats.orphanServiceIndexes++
		}
		stats.serviceFeedIndexes++
		return nil
	}); err != nil {
		return stats, err
	}

	var groupOwner uuid.UUID
	var groupSecond int64
	groupCount := 0
	canonicalIndexes := 0
	finishSameSecond := func() {
		if groupCount > 1 {
			stats.sameSecondGroups++
			stats.sameSecondEntries += groupCount
		}
	}
	if err := model.EntryIndex.Iter(db, func(key, value []byte) error {
		owner, _, _, err := model.ParseEntryIndexKey(key)
		if err != nil {
			stats.noncanonicalIndexes++
			stats.entryIndexes++
			return nil
		}
		canonicalIndexes++
		// EntryIndex is ordered by owner then reverse Unix ms. Grouping on the
		// eight reverse-time bytes detects collision-prone direct-index
		// positions without reading Entry.
		const reverseTimestampOffset = 4 + uuid.Size
		second := int64(binary.BigEndian.Uint64(key[reverseTimestampOffset : reverseTimestampOffset+8]))
		if groupCount > 0 && (owner != groupOwner || second != groupSecond) {
			finishSameSecond()
			groupCount = 0
		}
		groupOwner, groupSecond = owner, second
		groupCount++
		stats.entryIndexes++
		return nil
	}); err != nil {
		return stats, err
	}
	finishSameSecond()
	// Every healthy direct index was found by its exact deterministic key while
	// scanning Entry. Any additional canonical EntryIndex row is therefore an
	// orphan; this avoids reading Entry once for every index row.
	stats.orphanIndexes = canonicalIndexes - foundDirectIndexes
	if stats.orphanIndexes < 0 || foundDirectIndexes > expectedDirectIndexes {
		return stats, errors.New("direct index accounting is inconsistent")
	}

	// Keep only timestamp-mismatched pairs without their canonical index.
	// Healthy pairs and duplicate indexes are accounted for with counters.
	timelineMismatchedOnly := make(map[[2]uuid.UUID]struct{})
	matchedTimelinePositions := 0
	var timelineViewer uuid.UUID
	timelineViewerRows := 0
	timelineViewerActive := false
	finishTimelineViewer := func() {
		if timelineViewerRows == 0 {
			return
		}
		stats.timelineViewers++
		if !timelineViewerActive {
			stats.timelineInactiveRows += timelineViewerRows
		}
		limit := model.TimelineMaxEntries
		if model.IsPublicTimeline(timelineViewer) {
			limit = model.PublicTimelineMaxEntries
		} else if !timelineViewerActive {
			limit = model.TimelineColdEntries
		}
		if timelineViewerRows > limit {
			stats.timelineOverLimit++
		}
	}
	if err := model.TimelineIndex.Iter(db, func(key, _ []byte) error {
		viewer, entry, activity, err := model.ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		if timelineViewerRows == 0 || viewer != timelineViewer {
			finishTimelineViewer()
			timelineViewer, timelineViewerRows = viewer, 0
			// The public timeline has no TimelineState and never decays.
			timelineViewerActive = model.IsPublicTimeline(viewer)
			if !timelineViewerActive {
				timelineViewerActive, err = model.TimelineIsActive(db, viewer, time.Now().UTC())
				if err != nil {
					return err
				}
			}
		}
		timelineViewerRows++
		if _, err := db.Get(model.Entry.PrefixAppend(entry.Bytes())); errors.Is(err, store.ErrNotFound) {
			stats.timelineMissingEntry++
		} else if err != nil {
			return err
		}
		position, err := model.TimelinePositionTime(db, viewer, entry)
		if errors.Is(err, store.ErrNotFound) {
			stats.timelineMissingPos++
		} else if err != nil {
			return err
		} else if !position.Equal(activity) {
			canonical, err := model.TimelineIndexKey(viewer, entry, position)
			if err != nil {
				return err
			}
			exists, err := db.Exists(canonical)
			if err != nil {
				return err
			}
			if exists {
				stats.timelineDuplicates++
			} else {
				stats.timelineTimeMismatch++
				timelineMismatchedOnly[[2]uuid.UUID{viewer, entry}] = struct{}{}
			}
		} else {
			matchedTimelinePositions++
		}
		stats.timelineIndexes++
		return nil
	}); err != nil {
		return stats, err
	}
	finishTimelineViewer()
	if err := model.TimelinePosition.Iter(db, func(key, value []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid TimelinePosition key length %d", len(key))
		}
		if len(value) != 8 {
			return fmt.Errorf("invalid TimelinePosition value length %d", len(value))
		}
		ms := binary.BigEndian.Uint64(value)
		if ms > uint64(^uint64(0)>>1) {
			return errors.New("timeline position overflows int64")
		}
		stats.timelinePositions++
		return nil
	}); err != nil {
		return stats, err
	}
	// A Position is accounted for either by its exact index or by one known
	// timestamp-mismatched index. Everything left has no index at all.
	stats.timelineMissingIndex = stats.timelinePositions - matchedTimelinePositions - len(timelineMismatchedOnly)
	if stats.timelineMissingIndex < 0 {
		return stats, errors.New("timeline position accounting is inconsistent")
	}
	taskStats, err := taskqueue.Audit(db)
	if err == nil {
		err = auditInteractionTimelines(db, &stats)
	}
	if err == nil {
		err = auditGroups(db, &stats)
	}
	if err == nil {
		err = auditNotifications(db, &stats)
	}
	stats.tasks = taskStats
	return stats, err
}

func auditNotifications(db *store.Store, stats *storeAuditStats) error {
	var currentRecipient uuid.UUID
	var recipientRows, recipientUnread uint32
	var currentState model.NotificationStateRecord
	finishRecipient := func() error {
		if currentRecipient == uuid.Nil {
			return nil
		}
		if currentState.TotalCount != recipientRows || currentState.UnreadCount != recipientUnread {
			stats.notificationStateMismatch++
		}
		if recipientRows > model.NotificationMaxEntries {
			stats.notificationOverMax++
		}
		if recipientRows > model.NotificationTrimTrigger {
			stats.notificationOverTrigger++
		}
		return nil
	}

	if err := model.Notification.Iter(db, func(key, raw []byte) error {
		stats.notifications++
		if len(key) != model.Notification.Prefix.Len()+2*uuid.Size {
			stats.invalidNotifications++
			return nil
		}
		suffix := key[model.Notification.Prefix.Len():]
		recipient, recipientErr := uuid.FromBytes(suffix[:uuid.Size])
		notificationID, idErr := uuid.FromBytes(suffix[uuid.Size:])
		if recipientErr != nil || idErr != nil || recipient == uuid.Nil || notificationID == uuid.Nil {
			stats.invalidNotifications++
			return nil
		}
		if currentRecipient != uuid.Nil && recipient != currentRecipient {
			if err := finishRecipient(); err != nil {
				return err
			}
			currentRecipient, recipientRows, recipientUnread = uuid.Nil, 0, 0
			currentState = model.NotificationStateRecord{}
		}
		if currentRecipient == uuid.Nil {
			currentRecipient = recipient
			var err error
			currentState, err = model.GetNotificationState(db, recipient)
			if err != nil {
				return err
			}
			profile, err := model.GetProfileFromUuid(db, recipient)
			if err != nil || profile.Type != "user" || profile.Deleted {
				stats.orphanNotificationRecipients++
			}
		}

		var record model.NotificationRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			stats.invalidNotifications++
			return nil
		}
		if record.Version != model.NotificationRecordVersion || record.ID != notificationID.String() || record.RecipientUUID != recipient.String() {
			stats.invalidNotifications++
		}
		inboxKey, err := model.NotificationInboxKey(recipient, notificationID, time.UnixMilli(record.ActivityAtMS).UTC())
		if err != nil {
			stats.invalidNotifications++
			return nil
		}
		exists, err := db.Exists(inboxKey)
		if err != nil {
			return err
		}
		if !exists {
			stats.missingNotificationInbox++
		}
		recipientRows++
		if record.CreatedAtNS > currentState.LastReadAtNS {
			recipientUnread++
		}
		return nil
	}); err != nil {
		return err
	}
	if err := finishRecipient(); err != nil {
		return err
	}

	if err := model.NotificationInbox.Iter(db, func(key, _ []byte) error {
		stats.notificationInboxes++
		recipient, notificationID, activity, err := model.ParseNotificationInboxKey(key)
		if err != nil {
			stats.notificationInboxMismatch++
			return nil
		}
		record, err := model.GetNotification(db, recipient, notificationID)
		if errors.Is(err, model.ErrNotFound) {
			stats.orphanNotificationInbox++
			return nil
		}
		if err != nil {
			return err
		}
		if record.RecipientUUID != recipient.String() || record.ID != notificationID.String() || record.ActivityAtMS != activity.UnixMilli() {
			stats.notificationInboxMismatch++
		}
		return nil
	}); err != nil {
		return err
	}

	return model.NotificationState.Iter(db, func(key, raw []byte) error {
		stats.notificationStates++
		if len(key) != model.NotificationState.Prefix.Len()+uuid.Size {
			stats.notificationStateMismatch++
			return nil
		}
		var state model.NotificationStateRecord
		if err := json.Unmarshal(raw, &state); err != nil || state.Version != model.NotificationStateVersion {
			stats.notificationStateMismatch++
			return nil
		}
		recipient, _ := uuid.FromBytes(key[model.NotificationState.Prefix.Len():])
		hasRows, err := prefixHasAny(db, model.NotificationPrefix(recipient))
		if err != nil {
			return err
		}
		if !hasRows && (state.TotalCount != 0 || state.UnreadCount != 0) {
			stats.notificationStateMismatch++
		}
		return nil
	})
}

func prefixHasAny(db *store.Store, prefix store.Key) (bool, error) {
	iter, err := db.NewIterator(prefix)
	if err != nil {
		return false, err
	}
	defer iter.Close()
	iter.First()
	if err := iter.Error(); err != nil {
		return false, err
	}
	return iter.Valid(), nil
}

func auditGroups(db *store.Store, stats *storeAuditStats) error {
	if err := model.Profile.Iter(db, func(key, raw []byte) error {
		profile := new(pb.Profile)
		if err := proto.Unmarshal(raw, profile); err != nil {
			return err
		}
		if profile.Type != "group" {
			return nil
		}
		stats.groups++
		if len(key) != model.Profile.Prefix.Len()+uuid.Size {
			return fmt.Errorf("invalid Group Profile key length %d", len(key))
		}
		group, err := uuid.FromBytes(key[model.Profile.Prefix.Len():])
		if err != nil {
			return err
		}
		adminPrefix := model.NewKeyFrom(model.GroupAdmin.Prefix, group.Bytes())
		if !profile.Deleted {
			_, indexCount, err := model.GroupIndexActivity(db, group)
			if err != nil {
				return err
			}
			if indexCount == 0 {
				stats.missingGroupIndexRows++
			} else if indexCount > 1 {
				stats.duplicateGroupIndexRows += indexCount - 1
			}
			hasAdmin, err := prefixHasAny(db, adminPrefix)
			if err != nil {
				return err
			}
			if !hasAdmin {
				stats.groupsWithoutAdmins++
			}
			return nil
		}
		for _, prefix := range []store.Key{
			adminPrefix,
			model.NewKeyFrom(model.Follower.Prefix, group.Bytes()),
			model.NewKeyFrom(model.FeedService.Prefix, group.Bytes()),
		} {
			hasRows, err := prefixHasAny(db, prefix)
			if err != nil {
				return err
			}
			if hasRows {
				stats.deletedGroupResiduals++
				break
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := model.GroupAdmin.Iter(db, func(key, _ []byte) error {
		stats.groupAdmins++
		if len(key) != model.GroupAdmin.Prefix.Len()+2*uuid.Size {
			stats.invalidGroupAdmins++
			return nil
		}
		suffix := key[model.GroupAdmin.Prefix.Len():]
		group, groupErr := uuid.FromBytes(suffix[:uuid.Size])
		admin, adminErr := uuid.FromBytes(suffix[uuid.Size:])
		if groupErr != nil || adminErr != nil || group == uuid.Nil || admin == uuid.Nil {
			stats.invalidGroupAdmins++
			return nil
		}
		groupProfile := new(pb.Profile)
		if err := model.Profile.Get(db, group.Bytes(), groupProfile); err != nil ||
			groupProfile.Type != "group" || groupProfile.Deleted {
			stats.invalidGroupAdmins++
			return nil
		}
		adminProfile, err := model.GetProfileFromUuid(db, admin)
		if err != nil || adminProfile.Type != "user" {
			stats.invalidGroupAdmins++
			return nil
		}
		follow, err := db.Exists(model.NewKeyFrom(model.Follow.Prefix, admin.Bytes(), group.Bytes()))
		if err != nil {
			return err
		}
		follower, err := db.Exists(model.NewKeyFrom(model.Follower.Prefix, group.Bytes(), admin.Bytes()))
		if err != nil {
			return err
		}
		if !follow || !follower {
			stats.adminMissingMember++
		}
		return nil
	}); err != nil {
		return err
	}

	return model.GroupIndex.Iter(db, func(key, _ []byte) error {
		stats.groupIndexRows++
		group, _, err := model.ParseGroupIndexKey(key)
		if err != nil {
			stats.invalidGroupIndexRows++
			return nil
		}
		profile, err := model.GetProfileFromUuid(db, group)
		if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrProfileDeleted) {
			stats.orphanGroupIndexRows++
			return nil
		}
		if err != nil {
			return err
		}
		if profile.Type != "group" {
			stats.orphanGroupIndexRows++
		}
		return nil
	})
}

func auditInteractionTimelines(db *store.Store, stats *storeAuditStats) error {
	if err := model.Like.Iter(db, func(key, raw []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Like key length %d", len(key))
		}
		like := new(pb.Like)
		if err := proto.Unmarshal(raw, like); err != nil {
			return err
		}
		if like.From == nil {
			return nil
		}
		actor, err := uuid.FromString(like.From.Uuid)
		if err != nil || actor == uuid.Nil {
			return nil
		}
		entry, _ := uuid.FromBytes(key[4:20])
		if _, err := db.Get(model.Entry.PrefixAppend(entry.Bytes())); errors.Is(err, store.ErrNotFound) {
			stats.interactionOrphans++
		} else if err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339, like.Date)
		if err != nil {
			return nil
		}
		index, err := model.LikeTimelineKey(actor, entry, at)
		if err != nil {
			return err
		}
		if _, err := db.Get(index); errors.Is(err, store.ErrNotFound) {
			stats.interactionMismatches++
		} else if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := model.Comment.Iter(db, func(key, raw []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Comment key length %d", len(key))
		}
		comment := new(pb.Comment)
		if err := proto.Unmarshal(raw, comment); err != nil {
			return err
		}
		if comment.From == nil {
			return nil
		}
		actor, err := uuid.FromString(comment.From.Uuid)
		if err != nil || actor == uuid.Nil {
			return nil
		}
		entry, _ := uuid.FromBytes(key[4:20])
		commentID, _ := uuid.FromBytes(key[20:36])
		if _, err := db.Get(model.Entry.PrefixAppend(entry.Bytes())); errors.Is(err, store.ErrNotFound) {
			stats.interactionOrphans++
		} else if err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339, comment.Date)
		if err != nil {
			return nil
		}
		position, err := db.Get(model.CommentTimelinePositionKey(actor, entry))
		if errors.Is(err, store.ErrNotFound) {
			stats.interactionMismatches++
			return nil
		}
		if err != nil {
			return err
		}
		posAt, posComment, err := model.DecodeCommentTimelinePosition(position)
		if err != nil {
			return err
		}
		if at.After(posAt) || (at.Equal(posAt) && commentID.String() > posComment.String()) {
			stats.interactionMismatches++
		}
		return nil
	}); err != nil {
		return err
	}
	if err := model.LikeTimeline.Iter(db, func(key, _ []byte) error {
		stats.likeTimelineRows++
		actor, entry, _, err := model.ParseInteractionTimelineKey(key, model.TableLikeTimeline)
		if err != nil {
			return err
		}
		if _, err := db.Get(model.LikeKey(entry, actor)); errors.Is(err, store.ErrNotFound) {
			stats.interactionOrphans++
		} else if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := model.CommentTimeline.Iter(db, func(key, value []byte) error {
		stats.commentTimelineRows++
		actor, entry, at, err := model.ParseInteractionTimelineKey(key, model.TableCommentTimeline)
		if err != nil {
			return err
		}
		if len(value) != uuid.Size {
			return fmt.Errorf("invalid CommentTimeline value length %d", len(value))
		}
		comment, _ := uuid.FromBytes(value)
		if _, err := db.Get(model.CommentKey(entry, comment)); errors.Is(err, store.ErrNotFound) {
			stats.interactionOrphans++
		} else if err != nil {
			return err
		}
		position, err := db.Get(model.CommentTimelinePositionKey(actor, entry))
		if errors.Is(err, store.ErrNotFound) {
			stats.interactionMismatches++
			return nil
		}
		if err != nil {
			return err
		}
		posAt, posComment, err := model.DecodeCommentTimelinePosition(position)
		if err != nil {
			return err
		}
		if !posAt.Equal(at) || posComment != comment {
			stats.interactionMismatches++
		}
		return nil
	}); err != nil {
		return err
	}
	return model.CommentTimelinePosition.Iter(db, func(key, value []byte) error {
		stats.commentPositionRows++
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid CommentTimelinePosition key length %d", len(key))
		}
		actor, _ := uuid.FromBytes(key[4:20])
		entry, _ := uuid.FromBytes(key[20:36])
		at, comment, err := model.DecodeCommentTimelinePosition(value)
		if err != nil {
			return err
		}
		index, err := model.CommentTimelineKey(actor, entry, at)
		if err != nil {
			return err
		}
		raw, err := db.Get(index)
		if errors.Is(err, store.ErrNotFound) {
			stats.interactionMismatches++
			return nil
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, comment.Bytes()) {
			stats.interactionMismatches++
		}
		return nil
	})
}

func writeStoreAudit(out io.Writer, stats storeAuditStats) {
	fmt.Fprintf(out, "entries=%d noncanonical_entries=%d entry_key_id_mismatches=%d entry_indexes=%d noncanonical_indexes=%d missing_direct=%d orphan_indexes=%d\n",
		stats.entries, stats.noncanonicalEntries, stats.entryKeyIDMismatches, stats.entryIndexes,
		stats.noncanonicalIndexes, stats.missingDirectIndexes, stats.orphanIndexes)
	fmt.Fprintf(out, "timeline_indexes=%d timeline_positions=%d missing_entry=%d missing_position=%d missing_index=%d duplicates=%d timestamp_mismatch=%d\n",
		stats.timelineIndexes, stats.timelinePositions, stats.timelineMissingEntry, stats.timelineMissingPos,
		stats.timelineMissingIndex, stats.timelineDuplicates, stats.timelineTimeMismatch)
	fmt.Fprintf(out, "timeline_viewers=%d inactive_rows=%d over_limit_viewers=%d\n",
		stats.timelineViewers, stats.timelineInactiveRows, stats.timelineOverLimit)
	fmt.Fprintf(out, "same_second_groups=%d same_second_entries=%d\n", stats.sameSecondGroups, stats.sameSecondEntries)
	fmt.Fprintf(out, "follow=%d follower=%d missing_follower=%d missing_follow=%d max_followers=%d\n",
		stats.followEdges, stats.followerEdges, stats.missingFollowerEdges, stats.missingFollowEdges, stats.maxFollowers)
	fmt.Fprintf(out, "services=%d states=%d feed_services=%d service_feed_indexes=%d dormant=%d state_missing_service=%d binding_missing_service=%d binding_missing_index=%d disabled_with_index=%d orphan_service_indexes=%d\n",
		stats.services, stats.serviceStates, stats.feedServices, stats.serviceFeedIndexes, stats.dormantServices,
		stats.stateMissingService, stats.bindingMissingSource, stats.bindingMissingIndex,
		stats.disabledWithIndex, stats.orphanServiceIndexes)
	fmt.Fprintf(out, "like_timeline=%d comment_timeline=%d comment_positions=%d interaction_orphans=%d interaction_mismatches=%d\n",
		stats.likeTimelineRows, stats.commentTimelineRows, stats.commentPositionRows,
		stats.interactionOrphans, stats.interactionMismatches)
	fmt.Fprintf(out, "groups=%d group_admins=%d invalid_group_admins=%d admin_missing_membership=%d groups_without_admins=%d deleted_group_residuals=%d\n",
		stats.groups, stats.groupAdmins, stats.invalidGroupAdmins, stats.adminMissingMember,
		stats.groupsWithoutAdmins, stats.deletedGroupResiduals)
	fmt.Fprintf(out, "group_index=%d invalid_group_index=%d orphan_group_index=%d missing_group_index=%d duplicate_group_index=%d\n",
		stats.groupIndexRows, stats.invalidGroupIndexRows, stats.orphanGroupIndexRows,
		stats.missingGroupIndexRows, stats.duplicateGroupIndexRows)
	fmt.Fprintf(out, "notifications=%d notification_inbox=%d notification_states=%d invalid_notifications=%d orphan_recipients=%d missing_inbox=%d orphan_inbox=%d inbox_mismatch=%d state_mismatch=%d over_max=%d over_trigger=%d\n",
		stats.notifications, stats.notificationInboxes, stats.notificationStates, stats.invalidNotifications,
		stats.orphanNotificationRecipients, stats.missingNotificationInbox, stats.orphanNotificationInbox,
		stats.notificationInboxMismatch, stats.notificationStateMismatch, stats.notificationOverMax,
		stats.notificationOverTrigger)
	fmt.Fprintf(out, "tasks=%d ready=%d leases=%d idem=%d done=%d missing_ready=%d missing_lease=%d missing_idem=%d orphan_ready=%d orphan_lease=%d orphan_idem=%d mismatched_ready=%d mismatched_lease=%d mismatched_idem=%d invalid_done=%d\n",
		stats.tasks.Tasks, stats.tasks.Ready, stats.tasks.Leases, stats.tasks.Idempotency, stats.tasks.Done,
		stats.tasks.MissingReady, stats.tasks.MissingLease, stats.tasks.MissingIdem,
		stats.tasks.OrphanReady, stats.tasks.OrphanLease, stats.tasks.OrphanIdem,
		stats.tasks.MismatchedReady, stats.tasks.MismatchedLease, stats.tasks.MismatchedIdem,
		stats.tasks.InvalidDone)
}
