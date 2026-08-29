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
	Warnings     []schemaBlocker
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
	addBlocker := func(name string, count int) {
		if count != 0 {
			result.Blockers = append(result.Blockers, schemaBlocker{Name: name, Count: count})
		}
	}
	addWarning := func(name string, count int) {
		if count != 0 {
			result.Warnings = append(result.Warnings, schemaBlocker{Name: name, Count: count})
		}
	}
	addBlocker("noncanonical_entries", audit.noncanonicalEntries)
	addBlocker("entry_key_id_mismatches", audit.entryKeyIDMismatches)
	addBlocker("noncanonical_indexes", audit.noncanonicalIndexes)
	addWarning("missing_direct_indexes", audit.missingDirectIndexes)
	addWarning("orphan_indexes", audit.orphanIndexes)
	addWarning("timeline_missing_entry", audit.timelineMissingEntry)
	addWarning("timeline_missing_position", audit.timelineMissingPos)
	addWarning("timeline_missing_index", audit.timelineMissingIndex)
	addWarning("timeline_duplicates", audit.timelineDuplicates)
	addWarning("timeline_time_mismatch", audit.timelineTimeMismatch)
	addWarning("missing_follower_edges", audit.missingFollowerEdges)
	addWarning("missing_follow_edges", audit.missingFollowEdges)
	addWarning("orphan_memberships", audit.orphanMemberships)
	addWarning("invalid_follow_requests", audit.invalidFollowRequests)
	addWarning("service_state_missing_source", audit.stateMissingService)
	addWarning("binding_missing_source", audit.bindingMissingSource)
	addWarning("binding_missing_index", audit.bindingMissingIndex)
	addWarning("disabled_binding_with_index", audit.disabledWithIndex)
	addWarning("orphan_service_indexes", audit.orphanServiceIndexes)
	addWarning("interaction_orphans", audit.interactionOrphans)
	addWarning("interaction_mismatches", audit.interactionMismatches)
	addWarning("invalid_group_admins", audit.invalidGroupAdmins)
	addWarning("admin_missing_membership", audit.adminMissingMember)
	addWarning("groups_without_admins", audit.groupsWithoutAdmins)
	addWarning("deleted_group_residuals", audit.deletedGroupResiduals)
	addWarning("invalid_group_index", audit.invalidGroupIndexRows)
	addWarning("orphan_group_index", audit.orphanGroupIndexRows)
	addWarning("missing_group_index", audit.missingGroupIndexRows)
	addWarning("duplicate_group_index", audit.duplicateGroupIndexRows)
	addWarning("invalid_notifications", audit.invalidNotifications)
	addWarning("orphan_notification_recipients", audit.orphanNotificationRecipients)
	addWarning("missing_notification_inbox", audit.missingNotificationInbox)
	addWarning("orphan_notification_inbox", audit.orphanNotificationInbox)
	addWarning("notification_inbox_mismatch", audit.notificationInboxMismatch)
	addWarning("notification_state_mismatch", audit.notificationStateMismatch)
	addWarning("task_missing_ready", audit.tasks.MissingReady)
	addWarning("task_missing_lease", audit.tasks.MissingLease)
	addWarning("task_missing_idem", audit.tasks.MissingIdem)
	addWarning("task_orphan_ready", audit.tasks.OrphanReady)
	addWarning("task_orphan_lease", audit.tasks.OrphanLease)
	addWarning("task_orphan_idem", audit.tasks.OrphanIdem)
	addWarning("task_mismatched_ready", audit.tasks.MismatchedReady)
	addWarning("task_mismatched_lease", audit.tasks.MismatchedLease)
	addWarning("task_mismatched_idem", audit.tasks.MismatchedIdem)
	addWarning("task_invalid_done", audit.tasks.InvalidDone)

	legacy, err := scanSchemaLegacyRows(db)
	if err != nil {
		return result, err
	}
	addBlocker("embedded_interaction_entries", legacy.embeddedInteractionEntries)
	addBlocker("legacy_media_profiles", legacy.legacyMediaProfiles)
	addBlocker("legacy_media_entries", legacy.legacyMediaEntries)
	addBlocker("legacy_default_pictures", legacy.legacyDefaultPictures)

	groupAuthors, err := migrateGroupEntryAuthors(db, groupEntryAuthorMigrationOptions{dryRun: true})
	if err != nil {
		return result, fmt.Errorf("verify Group entry authors: %w", err)
	}
	addBlocker("legacy_group_entry_authors", groupAuthors.fixed)
	addWarning("unresolved_group_entry_authors", groupAuthors.unresolved)

	publicCacheKey := model.NewUUIDKey(model.TableMeta, uuid.NewV5(uuid.NamespaceURL, "index:public:cache"))
	if _, err := db.Get(publicCacheKey); err == nil {
		addBlocker("retired_public_cache", 1)
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
	fmt.Fprintf(out, "application_schema=%s version=%d current=%d pebble_format=%s ready=%t blockers=%d warnings=%d\n",
		result.Schema.Status, result.Schema.Version, model.CurrentDBSchemaVersion,
		result.PebbleFormat, len(result.Blockers) == 0, len(result.Blockers), len(result.Warnings))
	for _, blocker := range result.Blockers {
		fmt.Fprintf(out, "blocker=%s count=%d\n", blocker.Name, blocker.Count)
		fmt.Fprintf(out, "action=%s\n", schemaBlockerAction(blocker.Name))
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(out, "warning=%s count=%d\n", warning.Name, warning.Count)
	}
	if len(result.Blockers) == 0 {
		fmt.Fprintln(out, "guidance=schema encoding is ready; warnings are non-blocking runtime drift; run stamp_schema to verify again and write the marker")
	} else {
		fmt.Fprintln(out, "guidance=resolve every blocker and rerun verify_schema; warnings may be handled separately with audit/rebuild tools")
	}
}

func writeSchemaInspection(out io.Writer, result schemaVerification) {
	fmt.Fprintf(out, "application_schema=%s version=%d current=%d pebble_format=%s\n",
		result.Schema.Status, result.Schema.Version, model.CurrentDBSchemaVersion, result.PebbleFormat)
	switch result.Schema.Status {
	case model.DBSchemaMissing:
		fmt.Fprintln(out, "guidance=run verify_schema; if ready, run stamp_schema")
	case model.DBSchemaCurrent:
		fmt.Fprintln(out, "guidance=application schema marker is current; no schema action is required")
	case model.DBSchemaOlder:
		fmt.Fprintln(out, "guidance=run the migration tools from the current release, then verify_schema and stamp_schema")
	case model.DBSchemaFuture:
		fmt.Fprintln(out, "guidance=do not open with this binary; upgrade to a release that supports the recorded schema")
	case model.DBSchemaMalformed:
		fmt.Fprintln(out, "guidance=do not stamp or overwrite the marker; inspect a backup and repair the corrupted marker deliberately")
	}
}

func schemaBlockerAction(name string) string {
	switch name {
	case "noncanonical_entries":
		return "run migrate_entry_keys on an offline copy, then rebuild_entry_index"
	case "entry_key_id_mismatches":
		return "inspect the reported Entry corruption; do not guess or auto-rewrite mismatched authoritative IDs"
	case "noncanonical_indexes":
		return "run rebuild_entry_index on an offline copy"
	case "embedded_interaction_entries":
		return "run migrate_interactions -dry-run, resolve duplicate errors if any, then apply"
	case "legacy_group_entry_authors":
		return "run migrate_group_entry_authors -dry-run, then apply; unresolved historical authors are reported only as warnings"
	case "legacy_media_profiles", "legacy_media_entries":
		return "run migrate_media_urls -dry-run, then apply"
	case "legacy_default_pictures":
		return "run fix_default_picture -dry-run, then apply"
	case "retired_public_cache":
		return "run purge_public_cache -dry-run, then apply"
	default:
		return "inspect this blocker with audit_store before making changes"
	}
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
