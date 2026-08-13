package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

const searchIndexBatchSize = 500

type searchIndexOptions struct {
	user   string
	dryRun bool
}

type searchIndexStats struct {
	profiles int
	entries  int
	missing  int
	noBody   int
}

// oauthActiveProfiles returns non-deleted profiles and the subset with OAuth
// identity. Search indexing and offline Home prewarming intentionally use the
// same real-user definition; runtime Home fanout still follows TimelineState.
func oauthActiveProfiles(db *store.Store) (profiles []*pb.Profile, activeProfiles map[uuid.UUID]struct{}, err error) {
	profilesByID := make(map[string]uuid.UUID)
	if err := model.Profile.Iter(db, func(key, raw []byte) error {
		profile := new(pb.Profile)
		if err := proto.Unmarshal(raw, profile); err != nil {
			return fmt.Errorf("decode profile at %x: %w", key, err)
		}
		if profile.Deleted {
			return nil
		}
		profileID, err := uuid.FromString(profile.Uuid)
		if err != nil {
			return fmt.Errorf("profile %q has invalid UUID: %w", profile.Id, err)
		}
		profiles = append(profiles, profile)
		profilesByID[profile.Id] = profileID
		return nil
	}); err != nil {
		return nil, nil, err
	}
	activeProfiles = make(map[uuid.UUID]struct{})
	if err := model.OAuth.Iter(db, func(key, raw []byte) error {
		oauth := new(pb.OAuthUser)
		if err := proto.Unmarshal(raw, oauth); err != nil {
			return fmt.Errorf("decode OAuth record at %x: %w", key, err)
		}
		profileID, err := uuid.FromString(oauth.Uuid)
		if err == nil {
			activeProfiles[profileID] = struct{}{}
			return nil
		}
		if profileID, exists := profilesByID[oauth.Name]; exists {
			activeProfiles[profileID] = struct{}{}
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return profiles, activeProfiles, nil
}

// rebuildSearchIndex indexes every historical entry authored by profiles that
// carry OAuth login information. The author index (EntryIndex keyed by profile
// UUID) holds one row per entry, so each entry is indexed exactly once. idx
// must be nil in dry-run mode: the search index is never opened then.
func rebuildSearchIndex(db *store.Store, idx bleve.Index, options searchIndexOptions) (searchIndexStats, error) {
	stats := searchIndexStats{}
	if !options.dryRun && idx == nil {
		return stats, errors.New("search index is required outside dry-run")
	}

	profiles, activeProfiles, err := oauthActiveProfiles(db)
	if err != nil {
		return stats, err
	}
	if len(activeProfiles) == 0 && options.user == "" {
		return stats, errors.New("no profiles with OAuth information found")
	}

	var batch *bleve.Batch
	batchCount := 0
	if !options.dryRun {
		batch = idx.NewBatch()
	}
	flush := func() error {
		if options.dryRun || batchCount == 0 {
			return nil
		}
		if err := idx.Batch(batch); err != nil {
			return fmt.Errorf("flush search index batch: %w", err)
		}
		batch = idx.NewBatch()
		batchCount = 0
		return nil
	}

	for _, profile := range profiles {
		if options.user != "" && profile.Id != options.user {
			continue
		}
		profileID, err := uuid.FromString(profile.Uuid)
		if err != nil {
			return stats, fmt.Errorf("profile %q has invalid UUID: %w", profile.Id, err)
		}
		if options.user != "" {
			log.Printf("explicit user %s selected; OAuth metadata check bypassed", profile.Id)
		} else if _, active := activeProfiles[profileID]; !active {
			continue
		}

		indexed := 0
		authorPrefix := model.NewUUIDKey(model.TableEntryIndex, profileID)
		if _, err := db.ForwardScan(authorPrefix, func(i int, key, value []byte) error {
			_, entryID, _, err := model.ParseEntryIndexKey(key)
			if err != nil {
				return err
			}
			entryKey := model.Entry.PrefixAppend(entryID.Bytes())
			raw, err := db.Get(entryKey)
			if errors.Is(err, store.ErrNotFound) {
				stats.missing++
				return nil
			}
			if err != nil {
				return fmt.Errorf("read entry at %x: %w", entryKey, err)
			}
			entry := new(pb.Entry)
			if err := proto.Unmarshal(raw, entry); err != nil {
				return fmt.Errorf("decode entry at %x: %w", entryKey, err)
			}
			// Mirror PutEntry: only entries with a body are searchable.
			if entry.Body == "" {
				stats.noBody++
				return nil
			}
			if !options.dryRun {
				if err := batch.Index(entry.Id, entry.Body); err != nil {
					return fmt.Errorf("index entry %s: %w", entry.Id, err)
				}
				batchCount++
			}
			indexed++
			stats.entries++
			if batchCount >= searchIndexBatchSize {
				return flush()
			}
			return nil
		}); err != nil {
			return stats, fmt.Errorf("index entries for %s: %w", profile.Id, err)
		}
		if err := flush(); err != nil {
			return stats, err
		}

		stats.profiles++
		action := "indexed"
		if options.dryRun {
			action = "would index"
		}
		log.Printf("%s %d entries for %s", action, indexed, profile.Id)
	}
	if options.user != "" && stats.profiles == 0 {
		return stats, fmt.Errorf("profile %q not found", options.user)
	}
	return stats, nil
}

func runRebuildSearchIndexCommand(ndb *store.Store, indexPath string) {
	var idx bleve.Index
	if !dryRun {
		var err error
		idx, err = search.OpenIndex(indexPath)
		if err != nil {
			log.Fatalf("open search index %s: %v", indexPath, err)
		}
		defer func() {
			if err := idx.Close(); err != nil {
				log.Printf("close search index: %v", err)
			}
		}()
	}

	stats, err := rebuildSearchIndex(ndb, idx, searchIndexOptions{user: timelineUser, dryRun: dryRun})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("search index summary: %d profiles, %d entries indexed, %d missing entries, %d entries without body, dry-run=%t",
		stats.profiles, stats.entries, stats.missing, stats.noBody, dryRun)
}
