package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type schemaBlocker struct {
	Name  string
	Count int
}

type schemaVerification struct {
	Schema       model.DBSchemaInfo
	PebbleFormat string
	Blockers     []schemaBlocker
}

type schemaLegacyStats struct {
	embeddedInteractionEntries int
	legacyMediaProfiles        int
	legacyMediaEntries         int
	legacyDefaultPictures      int
}

func inspectSchema(db *store.Store) (schemaVerification, error) {
	info, err := model.InspectDBSchema(db)
	if err != nil {
		return schemaVerification{}, err
	}
	return schemaVerification{Schema: info, PebbleFormat: db.FormatMajorVersion().String()}, nil
}

func verifySchema(db *store.Store) (schemaVerification, error) {
	result, err := inspectSchema(db)
	if err != nil {
		return result, err
	}
	info := result.Schema
	if info.Status == model.DBSchemaMalformed || info.Status == model.DBSchemaFuture {
		return result, fmt.Errorf("database schema marker is %s (version=%d)", info.Status, info.Version)
	}

	audit, err := auditStore(db)
	if err != nil {
		return result, err
	}
	add := func(name string, count int) {
		if count != 0 {
			result.Blockers = append(result.Blockers, schemaBlocker{Name: name, Count: count})
		}
	}
	add("noncanonical_entries", audit.noncanonicalEntries)
	add("entry_key_id_mismatches", audit.entryKeyIDMismatches)
	add("noncanonical_indexes", audit.noncanonicalIndexes)
	add("missing_direct_indexes", audit.missingDirectIndexes)
	add("orphan_indexes", audit.orphanIndexes)
	add("timeline_missing_entry", audit.timelineMissingEntry)
	add("timeline_missing_position", audit.timelineMissingPos)
	add("timeline_missing_index", audit.timelineMissingIndex)
	add("timeline_duplicates", audit.timelineDuplicates)
	add("timeline_time_mismatch", audit.timelineTimeMismatch)
	add("missing_follower_edges", audit.missingFollowerEdges)
	add("missing_follow_edges", audit.missingFollowEdges)
	add("orphan_memberships", audit.orphanMemberships)
	add("invalid_follow_requests", audit.invalidFollowRequests)
	add("service_state_missing_source", audit.stateMissingService)
	add("binding_missing_source", audit.bindingMissingSource)
	add("binding_missing_index", audit.bindingMissingIndex)
	add("disabled_binding_with_index", audit.disabledWithIndex)
	add("orphan_service_indexes", audit.orphanServiceIndexes)
	add("interaction_orphans", audit.interactionOrphans)
	add("interaction_mismatches", audit.interactionMismatches)
	add("invalid_group_admins", audit.invalidGroupAdmins)
	add("admin_missing_membership", audit.adminMissingMember)
	add("groups_without_admins", audit.groupsWithoutAdmins)
	add("deleted_group_residuals", audit.deletedGroupResiduals)
	add("invalid_group_index", audit.invalidGroupIndexRows)
	add("orphan_group_index", audit.orphanGroupIndexRows)
	add("missing_group_index", audit.missingGroupIndexRows)
	add("duplicate_group_index", audit.duplicateGroupIndexRows)
	add("invalid_notifications", audit.invalidNotifications)
	add("orphan_notification_recipients", audit.orphanNotificationRecipients)
	add("missing_notification_inbox", audit.missingNotificationInbox)
	add("orphan_notification_inbox", audit.orphanNotificationInbox)
	add("notification_inbox_mismatch", audit.notificationInboxMismatch)
	add("notification_state_mismatch", audit.notificationStateMismatch)
	add("task_missing_ready", audit.tasks.MissingReady)
	add("task_missing_lease", audit.tasks.MissingLease)
	add("task_missing_idem", audit.tasks.MissingIdem)
	add("task_orphan_ready", audit.tasks.OrphanReady)
	add("task_orphan_lease", audit.tasks.OrphanLease)
	add("task_orphan_idem", audit.tasks.OrphanIdem)
	add("task_mismatched_ready", audit.tasks.MismatchedReady)
	add("task_mismatched_lease", audit.tasks.MismatchedLease)
	add("task_mismatched_idem", audit.tasks.MismatchedIdem)
	add("task_invalid_done", audit.tasks.InvalidDone)

	legacy, err := scanSchemaLegacyRows(db)
	if err != nil {
		return result, err
	}
	add("embedded_interaction_entries", legacy.embeddedInteractionEntries)
	add("legacy_media_profiles", legacy.legacyMediaProfiles)
	add("legacy_media_entries", legacy.legacyMediaEntries)
	add("legacy_default_pictures", legacy.legacyDefaultPictures)

	groupAuthors, err := migrateGroupEntryAuthors(db, groupEntryAuthorMigrationOptions{dryRun: true})
	if err != nil {
		return result, fmt.Errorf("verify Group entry authors: %w", err)
	}
	add("legacy_group_entry_authors", groupAuthors.candidates)
	add("unresolved_group_entry_authors", groupAuthors.unresolved)

	publicCacheKey := model.NewUUIDKey(model.TableMeta, uuid.NewV5(uuid.NamespaceURL, "index:public:cache"))
	if _, err := db.Get(publicCacheKey); err == nil {
		add("retired_public_cache", 1)
	} else if !errors.Is(err, store.ErrNotFound) {
		return result, fmt.Errorf("verify retired public cache: %w", err)
	}
	return result, nil
}

func scanSchemaLegacyRows(db *store.Store) (schemaLegacyStats, error) {
	stats := schemaLegacyStats{}
	if err := model.Profile.Iter(db, func(key, raw []byte) error {
		profile := new(pb.Profile)
		if err := proto.Unmarshal(raw, profile); err != nil {
			return fmt.Errorf("decode Profile[%x]: %w", key, err)
		}
		if _, legacy := migrateMediaURL(profile.Picture); legacy {
			stats.legacyMediaProfiles++
		}
		if isLegacyDefaultPicture(profile.Picture) {
			stats.legacyDefaultPictures++
		}
		return nil
	}); err != nil {
		return stats, fmt.Errorf("verify legacy Profile rows: %w", err)
	}
	if err := model.Entry.Iter(db, func(key, raw []byte) error {
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode Entry[%x]: %w", key, err)
		}
		if len(entry.Likes) != 0 || len(entry.Comments) != 0 {
			stats.embeddedInteractionEntries++
		}
		legacyMedia := false
		for _, thumbnail := range entry.Thumbnails {
			if thumbnail == nil {
				continue
			}
			if _, legacy := migrateMediaURL(thumbnail.Url); legacy {
				legacyMedia = true
			}
			if _, legacy := migrateMediaURL(thumbnail.Link); legacy {
				legacyMedia = true
			}
		}
		if _, legacy := migrateMediaText(entry.Body); legacy {
			legacyMedia = true
		}
		if _, legacy := migrateMediaText(entry.RawBody); legacy {
			legacyMedia = true
		}
		if legacyMedia {
			stats.legacyMediaEntries++
		}
		return nil
	}); err != nil {
		return stats, fmt.Errorf("verify legacy Entry rows: %w", err)
	}
	return stats, nil
}

func writeSchemaVerification(out io.Writer, result schemaVerification) {
	fmt.Fprintf(out, "application_schema=%s version=%d current=%d pebble_format=%s ready=%t\n",
		result.Schema.Status, result.Schema.Version, model.CurrentDBSchemaVersion,
		result.PebbleFormat, len(result.Blockers) == 0)
	for _, blocker := range result.Blockers {
		fmt.Fprintf(out, "blocker=%s count=%d\n", blocker.Name, blocker.Count)
	}
}

func writeSchemaInspection(out io.Writer, result schemaVerification) {
	fmt.Fprintf(out, "application_schema=%s version=%d current=%d pebble_format=%s\n",
		result.Schema.Status, result.Schema.Version, model.CurrentDBSchemaVersion, result.PebbleFormat)
}

func stampSchema(db *store.Store, dryRun bool) (schemaVerification, bool, error) {
	result, err := verifySchema(db)
	if err != nil {
		return result, false, err
	}
	if len(result.Blockers) != 0 {
		return result, false, fmt.Errorf("database is not ready for application schema %d: %d blocker classes", model.CurrentDBSchemaVersion, len(result.Blockers))
	}
	if dryRun || result.Schema.Status == model.DBSchemaCurrent {
		return result, false, nil
	}
	if err := model.PutDBSchemaVersion(db, model.CurrentDBSchemaVersion); err != nil {
		return result, false, err
	}
	return result, true, nil
}
