package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

var errGroupEntryAuthorLimitReached = errors.New("group entry author migration limit reached")

type groupEntryAuthorMigrationOptions struct {
	user     string
	maxLimit int
	dryRun   bool
}

type groupEntryAuthorMigrationStats struct {
	entriesScanned int
	candidates     int
	fixed          int
	unresolved     int
	skipped        int
}

// migrateGroupEntryAuthors repairs entries imported by the historical
// archiver, which replaced the real author UUID with the Group UUID while
// retaining the real author in From.Id. It deliberately accepts only records
// whose current author and target are the same Group and whose From.Id proves
// a distinct, current user through UserMap -> Profile.
func migrateGroupEntryAuthors(db *store.Store, options groupEntryAuthorMigrationOptions) (groupEntryAuthorMigrationStats, error) {
	stats := groupEntryAuthorMigrationStats{}
	err := model.Entry.Iter(db, func(key, raw []byte) error {
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode Entry[%x]: %w", key, err)
		}
		if options.user != "" && entry.GetFrom().GetId() != options.user {
			return nil
		}
		if options.maxLimit > 0 && stats.entriesScanned >= options.maxLimit {
			return errGroupEntryAuthorLimitReached
		}
		stats.entriesScanned++

		groupID, err := uuid.FromString(entry.ProfileUuid)
		if err != nil || groupID == uuid.Nil {
			stats.skipped++
			return nil
		}
		group := new(pb.Profile)
		if err := model.Profile.Get(db, groupID.Bytes(), group); err != nil || group.Type != "group" {
			stats.skipped++
			return nil
		}
		if entry.FeedUuid != "" && entry.FeedUuid != entry.ProfileUuid {
			stats.skipped++
			return nil
		}
		if entry.GetFrom().GetId() == "" || entry.GetFrom().GetId() == group.Id {
			stats.skipped++
			return nil
		}
		stats.candidates++

		author, err := model.GetProfileFromUserId(db, entry.From.Id)
		if err != nil || author.Type != "user" {
			stats.unresolved++
			return nil
		}
		authorID, err := uuid.FromString(author.Uuid)
		if err != nil || authorID == uuid.Nil || authorID == groupID {
			stats.unresolved++
			return nil
		}
		entryID, err := uuid.FromString(entry.Id)
		if err != nil {
			return fmt.Errorf("entry %q UUID: %w", entry.Id, err)
		}
		canonicalKey := model.Entry.PrefixAppend(entryID.Bytes())
		if !bytes.Equal(key, canonicalKey) {
			return fmt.Errorf("entry %q has noncanonical key %x; run migrate_entry_keys", entry.Id, key)
		}
		published, err := time.Parse(time.RFC3339, entry.Date)
		if err != nil {
			return fmt.Errorf("entry %q date: %w", entry.Id, err)
		}

		entry.ProfileUuid = author.Uuid
		entry.From.Uuid = author.Uuid
		// FeedUuid remains the Group UUID. Empty is the oldest legacy shape;
		// make the target explicit while repairing the author.
		entry.FeedUuid = groupID.String()
		encoded, err := proto.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode entry %q: %w", entry.Id, err)
		}
		authorIndex, err := model.EntryIndexKey(authorID, entryID, published)
		if err != nil {
			return fmt.Errorf("index entry %q for author: %w", entry.Id, err)
		}
		stats.fixed++
		if options.dryRun {
			return nil
		}
		return db.ApplyBatch(func(batch *pebble.Batch) error {
			if err := batch.Set(canonicalKey, encoded, nil); err != nil {
				return fmt.Errorf("write entry %q: %w", entry.Id, err)
			}
			if err := batch.Set(authorIndex, nil, nil); err != nil {
				return fmt.Errorf("write author index for entry %q: %w", entry.Id, err)
			}
			return nil
		})
	})
	if errors.Is(err, errGroupEntryAuthorLimitReached) {
		err = nil
	}
	return stats, err
}
