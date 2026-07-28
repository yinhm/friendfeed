package main

import (
	"errors"
	"fmt"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

var errActorBackfillLimitReached = errors.New("actor UUID backfill limit reached")

type actorUUIDBackfillOptions struct {
	user     string
	maxLimit int
	dryRun   bool
}

type actorUUIDBackfillStats struct {
	entriesScanned int
	entriesChanged int
	entryAuthors   int
	comments       int
	likes          int
	alreadySet     int
	unresolved     int
	conflicts      int
}

type actorResolution struct {
	uuid string
	ok   bool
}

type actorResolver struct {
	db      *store.Store
	results map[string]actorResolution
}

func newActorResolver(db *store.Store) *actorResolver {
	return &actorResolver{
		db:      db,
		results: make(map[string]actorResolution),
	}
}

// resolve uses the explicitly confirmed migration invariant that historical
// From.Id values on these databases still identify their original owners.
// The full UserMap -> Profile chain must nevertheless agree before a UUID is
// considered safe to write.
func (r *actorResolver) resolve(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	if result, found := r.results[id]; found {
		return result.uuid, result.ok
	}

	result := actorResolution{}
	rawUUID, err := r.db.Get(model.NewKeyFrom(model.TableUserMap.Bytes(), []byte(id)))
	if err == nil && len(rawUUID) > 0 {
		profileUUID, parseErr := uuid.FromBytes(rawUUID)
		if parseErr == nil && profileUUID != uuid.Nil {
			profile, profileErr := model.GetProfileFromUuid(r.db, profileUUID)
			storedUUID, storedErr := uuid.FromString(profile.GetUuid())
			if profileErr == nil && profile != nil && profile.Id == id &&
				storedErr == nil && storedUUID == profileUUID {
				result = actorResolution{uuid: profileUUID.String(), ok: true}
			}
		}
	}
	r.results[id] = result
	return result.uuid, result.ok
}

func existingUUIDMatches(value, resolved string) (empty, matches bool) {
	if value == "" {
		return true, false
	}
	current, err := uuid.FromString(value)
	if err != nil || current == uuid.Nil {
		return false, false
	}
	want, err := uuid.FromString(resolved)
	return false, err == nil && want != uuid.Nil && current == want
}

func backfillFeedActor(from *pb.Feed, resolver *actorResolver, stats *actorUUIDBackfillStats) bool {
	if from == nil {
		stats.unresolved++
		return false
	}
	resolved, ok := resolver.resolve(from.Id)
	if !ok {
		stats.unresolved++
		return false
	}
	empty, matches := existingUUIDMatches(from.Uuid, resolved)
	switch {
	case empty:
		from.Uuid = resolved
		return true
	case matches:
		stats.alreadySet++
	default:
		stats.conflicts++
	}
	return false
}

func backfillEntryAuthor(entry *pb.Entry, resolver *actorResolver, stats *actorUUIDBackfillStats) bool {
	if entry.From == nil {
		stats.unresolved++
		return false
	}
	resolved, ok := resolver.resolve(entry.From.Id)
	if !ok {
		stats.unresolved++
		return false
	}

	fromEmpty, fromMatches := existingUUIDMatches(entry.From.Uuid, resolved)
	profileEmpty, profileMatches := existingUUIDMatches(entry.ProfileUuid, resolved)
	if (!fromEmpty && !fromMatches) || (!profileEmpty && !profileMatches) {
		stats.conflicts++
		return false
	}
	if !fromEmpty && !profileEmpty {
		stats.alreadySet++
		return false
	}
	entry.From.Uuid = resolved
	entry.ProfileUuid = resolved
	stats.entryAuthors++
	return true
}

func backfillActorUUIDs(db *store.Store, options actorUUIDBackfillOptions) (actorUUIDBackfillStats, error) {
	stats := actorUUIDBackfillStats{}
	resolver := newActorResolver(db)

	err := model.Entry.Iter(db, func(key, raw []byte) error {
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode entry at %x: %w", key, err)
		}
		if options.user != "" && (entry.From == nil || entry.From.Id != options.user) {
			return nil
		}
		if options.maxLimit > 0 && stats.entriesScanned >= options.maxLimit {
			return errActorBackfillLimitReached
		}
		stats.entriesScanned++

		changed := backfillEntryAuthor(entry, resolver, &stats)
		for _, comment := range entry.Comments {
			if comment == nil {
				stats.unresolved++
				continue
			}
			if backfillFeedActor(comment.From, resolver, &stats) {
				stats.comments++
				changed = true
			}
		}
		for _, like := range entry.Likes {
			if like == nil {
				stats.unresolved++
				continue
			}
			if backfillFeedActor(like.From, resolver, &stats) {
				stats.likes++
				changed = true
			}
		}
		if !changed {
			return nil
		}
		stats.entriesChanged++
		// Dry-run correctness depends on returning before db.Set. Flush is
		// not a write boundary: Pebble commits Set calls to WAL/memtable and
		// may flush them automatically.
		if options.dryRun {
			return nil
		}
		encoded, err := proto.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode entry %q: %w", entry.Id, err)
		}
		if err := db.Set(key, encoded); err != nil {
			return fmt.Errorf("write entry %q: %w", entry.Id, err)
		}
		return nil
	})
	if errors.Is(err, errActorBackfillLimitReached) {
		err = nil
	}
	return stats, err
}
