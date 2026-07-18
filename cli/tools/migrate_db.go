package main

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

var fromPath string
var toPath string
var command string
var timelineUser string
var timelineMaxFeeds int
var dryRun bool

func init() {
	flag.StringVar(&fromPath, "from", "", "from directory")
	flag.StringVar(&toPath, "to", "", "to directory")
	flag.StringVar(&command, "c", "", "command to do")
	flag.StringVar(&timelineUser, "user", "", "limit timeline rebuild to one profile ID")
	flag.IntVar(&timelineMaxFeeds, "max-feeds", 0, "maximum source feeds per timeline (0 is unlimited)")
	flag.BoolVar(&dryRun, "dry-run", false, "report timeline rebuild without writing changes")
}

func purge_table(db *store.Store, prefix store.Key) (int, error) {
	return db.ForwardScan(prefix, func(i int, k, v []byte) error {
		return db.Delete(k)
	})
}

const publicFeedIndexSize = 1000

// Before OAuth providers were merged into TableOAuth, Google OAuth records
// used table prefix 105. That prefix is TableFile in the current schema.
const legacyGoogleOAuthPrefix store.KeyPrefix = 105

func publicFeedIndexKey(prefix store.KeyPrefix, id uuid.UUID) store.Key {
	return model.NewUUIDKey(prefix, id)
}

func normalizePublicFeedIndex(raw []byte) ([]byte, int, error) {
	var entries []string
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&entries); err != nil {
		return nil, 0, err
	}

	normalized := make([]string, publicFeedIndexSize)
	copy(normalized, entries)

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(normalized); err != nil {
		return nil, 0, err
	}

	count := 0
	for _, entry := range normalized {
		if entry != "" {
			count++
		}
	}
	return buf.Bytes(), count, nil
}

func migratePublicFeedIndex(db, mdb, ndb *store.Store, overwrite bool) error {
	currentID := uuid.NewV5(uuid.NamespaceURL, "index:public:cache")
	currentKey := publicFeedIndexKey(model.TableMeta, currentID)
	if !overwrite && ndb.Exist(currentKey) {
		log.Println("public feed index already exists; skipping legacy metadata")
		return nil
	}

	zeroID := uuid.Nil
	legacyMetaKey := publicFeedIndexKey(model.TableMeta, zeroID)
	// Before TableIndexCache was removed, its numeric prefix was 6.
	legacyIndexCacheKey := publicFeedIndexKey(store.KeyPrefix(6), zeroID)
	candidates := []struct {
		name string
		db   *store.Store
		key  store.Key
	}{
		{"meta/current", mdb, currentKey},
		{"data/current", db, currentKey},
		{"meta/zero-uuid", mdb, legacyMetaKey},
		{"data/zero-uuid", db, legacyMetaKey},
		{"meta/index-cache", mdb, legacyIndexCacheKey},
		{"data/index-cache", db, legacyIndexCacheKey},
	}

	for _, candidate := range candidates {
		raw, err := candidate.db.Get(candidate.key)
		if err != nil {
			return fmt.Errorf("read public feed index from %s: %w", candidate.name, err)
		}
		if len(raw) == 0 {
			continue
		}

		normalized, count, err := normalizePublicFeedIndex(raw)
		if err != nil {
			return fmt.Errorf("decode public feed index from %s: %w", candidate.name, err)
		}
		if err := ndb.Set(currentKey, normalized); err != nil {
			return fmt.Errorf("write migrated public feed index: %w", err)
		}
		log.Printf("migrated public feed index from %s: %d entries", candidate.name, count)
		return nil
	}

	return errors.New("public feed index metadata not found in source databases")
}

type timelineRebuildStats struct {
	profiles int
	follows  int
	entries  int
	existing int
}

type timelineRebuildOptions struct {
	user     string
	maxFeeds int
	dryRun   bool
}

type socialGraphEdge struct {
	follower uuid.UUID
	feed     uuid.UUID
}

type socialGraphRebuildStats struct {
	feedinfos int
	edges     int
	skipped   int
}

type mediaURLMigrationStats struct {
	profiles   int
	entries    int
	thumbnails int
}

func migrateOAuthRecords(source, target *store.Store, prefix store.KeyPrefix, provider string) (int, error) {
	return source.ForwardScan(model.KeyPrefixToBytes(prefix), func(i int, key, raw []byte) error {
		msg := new(pb.OAuthUser)
		if err := proto.Unmarshal(raw, msg); err != nil {
			return fmt.Errorf("decode OAuth record at %x: %w", key, err)
		}
		if provider != "" {
			msg.Provider = provider
		}
		if msg.UserId == "" || msg.Provider == "" {
			return nil
		}
		if msg.Uuid == "" && msg.Name != "" {
			if profile, err := model.GetProfileFromUserId(target, msg.Name); err == nil {
				msg.Uuid = profile.Uuid
			}
		}
		log.Printf("migrate OAuth: provider=%s user_id=%s uuid=%s", msg.Provider, msg.UserId, msg.Uuid)
		if provider != "" {
			// The legacy provider table is authoritative. This also repairs an
			// incorrect row created by a login attempted before migration.
			if err := model.OAuth.Delete(target, model.KeyFromString(msg.Provider, msg.UserId)); err != nil {
				return fmt.Errorf("replace OAuth record %s:%s: %w", msg.Provider, msg.UserId, err)
			}
		}
		if _, err := model.PutOAuthUser(target, msg); err != nil {
			return fmt.Errorf("write OAuth record %s:%s: %w", msg.Provider, msg.UserId, err)
		}
		return nil
	})
}

func migrateMediaURL(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, false
	}

	switch strings.ToLower(parsed.Hostname()) {
	case "storage.googleapis.com":
		const bucketPrefix = "/lastff01/"
		if !strings.HasPrefix(parsed.Path, bucketPrefix) {
			return rawURL, false
		}
		parsed.Path = "/" + strings.TrimPrefix(parsed.Path, bucketPrefix)
	case "m.friendfeed-media.com":
		// The object path and URL scheme are already correct for R2.
	default:
		return rawURL, false
	}

	parsed.Host = "m.friendfeed.me"
	return parsed.String(), true
}

func migrateMediaURLs(db *store.Store, dryRun bool) (mediaURLMigrationStats, error) {
	stats := mediaURLMigrationStats{}

	if err := model.Profile.Iter(db, func(key, raw []byte) error {
		profile := new(pb.Profile)
		if err := proto.Unmarshal(raw, profile); err != nil {
			return fmt.Errorf("decode profile at %x: %w", key, err)
		}
		picture, changed := migrateMediaURL(profile.Picture)
		if !changed {
			return nil
		}
		profile.Picture = picture
		stats.profiles++
		if dryRun {
			return nil
		}
		value, err := proto.Marshal(profile)
		if err != nil {
			return fmt.Errorf("encode profile %q: %w", profile.Id, err)
		}
		if err := db.Set(key, value); err != nil {
			return fmt.Errorf("write migrated profile %q: %w", profile.Id, err)
		}
		return nil
	}); err != nil {
		return stats, err
	}

	if err := model.Entry.Iter(db, func(key, raw []byte) error {
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode entry at %x: %w", key, err)
		}
		changed := false
		for _, thumbnail := range entry.Thumbnails {
			if thumbnail == nil {
				continue
			}
			if migrated, ok := migrateMediaURL(thumbnail.Url); ok {
				thumbnail.Url = migrated
				stats.thumbnails++
				changed = true
			}
		}
		if !changed {
			return nil
		}
		stats.entries++
		if dryRun {
			return nil
		}
		value, err := proto.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode entry %q: %w", entry.Id, err)
		}
		if err := db.Set(key, value); err != nil {
			return fmt.Errorf("write migrated entry %q: %w", entry.Id, err)
		}
		return nil
	}); err != nil {
		return stats, err
	}
	return stats, nil
}

func rebuildSocialGraph(db *store.Store, dryRun bool) (socialGraphRebuildStats, error) {
	stats := socialGraphRebuildStats{}
	profilesByID := make(map[string]uuid.UUID)
	profilesByUUID := make(map[uuid.UUID]struct{})
	if err := model.Profile.Iter(db, func(key, raw []byte) error {
		profile := new(pb.Profile)
		if err := proto.Unmarshal(raw, profile); err != nil {
			return fmt.Errorf("decode profile at %x: %w", key, err)
		}
		if profile.Deleted {
			return nil
		}
		id, err := uuid.FromString(profile.Uuid)
		if err != nil {
			return fmt.Errorf("profile %q has invalid UUID: %w", profile.Id, err)
		}
		profilesByID[profile.Id] = id
		profilesByUUID[id] = struct{}{}
		return nil
	}); err != nil {
		return stats, err
	}

	resolveProfile := func(profile *pb.Profile) (uuid.UUID, bool) {
		if profile == nil {
			return uuid.Nil, false
		}
		if id, err := uuid.FromString(profile.Uuid); err == nil {
			if _, exists := profilesByUUID[id]; exists {
				return id, true
			}
		}
		id, exists := profilesByID[profile.Id]
		return id, exists
	}

	edges := make(map[socialGraphEdge]struct{})
	if err := model.Feedinfo.Iter(db, func(key, raw []byte) error {
		info := new(pb.Feedinfo)
		if err := proto.Unmarshal(raw, info); err != nil {
			return fmt.Errorf("decode legacy feedinfo at %x: %w", key, err)
		}
		owner, exists := resolveProfile(&pb.Profile{Uuid: info.Uuid, Id: info.Id})
		if !exists {
			stats.skipped++
			return nil
		}
		stats.feedinfos++

		// Historical Feedinfo data predates the protobuf rename:
		// field 10 (now Following) was subscribers, and field 11 (now
		// Followers) was subscriptions.
		for _, subscriber := range info.Following {
			if follower, ok := resolveProfile(subscriber); ok && follower != owner {
				edges[socialGraphEdge{follower: follower, feed: owner}] = struct{}{}
			} else {
				stats.skipped++
			}
		}
		for _, subscription := range info.Followers {
			if feed, ok := resolveProfile(subscription); ok && feed != owner {
				edges[socialGraphEdge{follower: owner, feed: feed}] = struct{}{}
			} else {
				stats.skipped++
			}
		}
		return nil
	}); err != nil {
		return stats, err
	}
	if stats.feedinfos == 0 {
		return stats, errors.New("no legacy feedinfo records found; refusing to clear social graph")
	}
	stats.edges = len(edges)
	if dryRun {
		return stats, nil
	}

	if _, err := purge_table(db, model.Follow.Prefix); err != nil {
		return stats, fmt.Errorf("clear Follow table: %w", err)
	}
	if _, err := purge_table(db, model.Follower.Prefix); err != nil {
		return stats, fmt.Errorf("clear Follower table: %w", err)
	}
	for edge := range edges {
		followKey := model.NewKeyFrom(model.Follow.Prefix, edge.follower.Bytes(), edge.feed.Bytes())
		followerKey := model.NewKeyFrom(model.Follower.Prefix, edge.feed.Bytes(), edge.follower.Bytes())
		if err := db.Set(followKey, []byte("1")); err != nil {
			return stats, fmt.Errorf("write Follow edge %s -> %s: %w", edge.follower, edge.feed, err)
		}
		if err := db.Set(followerKey, []byte("1")); err != nil {
			return stats, fmt.Errorf("write Follower edge %s <- %s: %w", edge.feed, edge.follower, err)
		}
	}
	return stats, nil
}

func rebuildTimelineForProfile(db *store.Store, profile *pb.Profile, options timelineRebuildOptions) (int, int, int, error) {
	profileID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("profile %q has invalid UUID: %w", profile.Id, err)
	}

	timelineID := model.UniqueKeyFrom(fmt.Sprintf("%x", profileID), "user", "timeline")
	timelinePrefix := model.NewUUIDKey(model.TableEntryIndex, timelineID)
	existing, err := db.ForwardScan(timelinePrefix, func(i int, key, value []byte) error {
		return nil
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("count timeline for %s: %w", profile.Id, err)
	}
	if !options.dryRun {
		if _, err := purge_table(db, timelinePrefix); err != nil {
			return 0, 0, existing, fmt.Errorf("clear timeline for %s: %w", profile.Id, err)
		}
	}

	feeds := []uuid.UUID{profileID}
	seenFeeds := map[uuid.UUID]struct{}{profileID: {}}
	selectedFollows := 0
	followPrefix := model.NewKeyFrom(model.Follow.Prefix, profileID.Bytes())
	_, err = db.ForwardScan(followPrefix, func(i int, key, value []byte) error {
		if options.maxFeeds > 0 && len(feeds) >= options.maxFeeds {
			return &store.Error{Code: store.StopIteration, Msg: "feed limit reached"}
		}
		feedID, err := uuid.FromBytes(key[len(followPrefix):])
		if err != nil {
			return fmt.Errorf("invalid follow key %x: %w", key, err)
		}
		if _, exists := seenFeeds[feedID]; !exists {
			seenFeeds[feedID] = struct{}{}
			feeds = append(feeds, feedID)
			selectedFollows++
		}
		return nil
	})
	if err != nil {
		return 0, 0, existing, fmt.Errorf("scan follows for %s: %w", profile.Id, err)
	}

	entries := 0
	for _, feedID := range feeds {
		feedPrefix := model.NewUUIDKey(model.TableEntryIndex, feedID)
		_, err := db.ForwardScan(feedPrefix, func(i int, key, value []byte) error {
			if !options.dryRun {
				indexSuffix := key[len(feedPrefix):]
				timelineKey := model.NewKeyFrom(timelinePrefix, indexSuffix)
				if err := db.Set(timelineKey, value); err != nil {
					return err
				}
			}
			entries++
			return nil
		})
		if err != nil {
			return selectedFollows, entries, existing, fmt.Errorf("copy feed %s into %s timeline: %w", feedID, profile.Id, err)
		}
	}

	return selectedFollows, entries, existing, nil
}

func rebuildTimelines(db *store.Store, options timelineRebuildOptions) (timelineRebuildStats, error) {
	stats := timelineRebuildStats{}
	var profiles []*pb.Profile
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
		return stats, err
	}

	activeProfiles := make(map[uuid.UUID]struct{})
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
		// Older OAuth rows were sometimes stored before their profile UUID
		// was bound. Their login name still matches Profile.Id.
		if profileID, exists := profilesByID[oauth.Name]; exists {
			activeProfiles[profileID] = struct{}{}
		}
		return nil
	}); err != nil {
		return stats, err
	}
	if len(activeProfiles) == 0 && options.user == "" {
		return stats, errors.New("no profiles with OAuth information found")
	}

	for _, profile := range profiles {
		if options.user != "" && profile.Id != options.user {
			continue
		}
		if options.user != "" {
			log.Printf("explicit user %s selected; OAuth metadata check bypassed", profile.Id)
		} else {
			profileID := profilesByID[profile.Id]
			if _, active := activeProfiles[profileID]; !active {
				continue
			}
		}

		follows, entries, existing, err := rebuildTimelineForProfile(db, profile, options)
		if err != nil {
			return stats, err
		}
		stats.profiles++
		stats.follows += follows
		stats.entries += entries
		stats.existing += existing
		action := "rebuilt"
		if options.dryRun {
			action = "would rebuild"
		}
		log.Printf("%s timeline for %s: %d existing entries, %d source feeds, %d source entries", action, profile.Id, existing, follows+1, entries)
	}
	if options.user != "" && stats.profiles == 0 {
		return stats, fmt.Errorf("profile %q not found", options.user)
	}
	return stats, nil
}

// We should open original db ad readonly mode.
//
// migrate meta db should and only need sync_meta
// ./tools -from old_db -to new_db -c sync_meta
// migrate only the legacy public feed cache metadata
// ./tools -from old_db -to new_db -c public_feed
// rebuild Home timelines from each profile's own feed and Follow edges
// ./tools -to new_db -c rebuild_timeline -user yinhm -max-feeds 20 -dry-run
// rebuild Follow and Follower tables from legacy Feedinfo metadata
// ./tools -to new_db -c rebuild_social_graph -dry-run
// migrate legacy GCS and FriendFeed media URLs in profiles and entries
// ./tools -to new_db -c migrate_media_urls -dry-run
//
// sync all data from db
// ./tools -from old_db -to new_db -c db
// sync all data from meta db
// ./tools -from old_db -to new_db -c meta
//
// Test Profile
// ./tools -from old_db -to new_db -c profile
// debug
// ./tools -from old_db -to new_db -c debug
func main() {
	flag.Parse()

	if toPath == "" {
		log.Fatal("-to is required")
	}
	needsSource := command != "rebuild_timeline" && command != "rebuild_social_graph" && command != "migrate_media_urls"
	if needsSource && fromPath == "" {
		log.Fatal("-from is required for command ", command)
	}

	ndb := store.NewStore(toPath)
	ndb.SetSync(false)
	var db, mdb *store.Store
	if needsSource {
		db = store.NewStore(fromPath)
		mdb = store.NewMetaStore(fromPath + "/meta")
	}

	switch command {
	case "db":
		prefix := []byte("")

		log.Println("iter db now...")

		// iter here
		iter := db.NewIterator(prefix)
		for iter.First(); iter.Valid(); iter.Next() {
			ndb.Set(iter.Key(), iter.Value())
		}
		iter.Close()
		ndb.Flush()
		log.Println("iter done...")
	case "meta":
		prefix := []byte("")
		log.Println("iter meta db now...")

		// iter here
		iter := mdb.NewIterator(prefix)
		for iter.First(); iter.Valid(); iter.Next() {
			ndb.Set(iter.Key(), iter.Value())
		}

		iter.Close()
		ndb.Flush()
		log.Println("iter done...")

		prefix = model.TableProfile.Bytes()
		n, err := mdb.ForwardScan(prefix, func(i int, k, v []byte) error {
			return ndb.Set(k, v)
		})
		if err != nil {
			log.Println("Error on scanning user profiles:", err)
		}
		log.Printf("Profiles: %d", n)
		ndb.Flush()

		// now test it
		n, err = mdb.ForwardScan(prefix, func(i int, k, v []byte) error {
			v2, err := ndb.Get(k)
			if err != nil {
				fmt.Println("value not fount")
				return err
			}

			if !bytes.Equal(v, v2) {
				fmt.Println(v)
				fmt.Println(v2)
				return errors.New("value not equal")
			}
			return nil
		})
		if err != nil {
			log.Println("Error on compare profiles:", err)
		}
		log.Printf("Profiles: %d", n)

	case "sync_meta":
		log.Println("scan meta db now...")

		n, err := mdb.ForwardScan(model.TableProfile.Bytes(), func(i int, k, v []byte) error {
			msg := &pb.Profile{}
			if err := proto.Unmarshal(v, msg); err != nil {
				return err
			} else {
				// ndb.Set(k, v)
				return model.UpdateProfile(ndb, msg) // use UpdateProfile also update id->uuid map
			}
		})
		if err != nil {
			log.Println(err)
		}
		log.Println("profile count: ", n)

		twitterCount, err := migrateOAuthRecords(mdb, ndb, model.TableOAuth, "")
		if err != nil {
			log.Fatal(err)
		}
		googleCount, err := migrateOAuthRecords(mdb, ndb, legacyGoogleOAuthPrefix, "google")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("oauth user count: twitter/current=%d legacy-google=%d", twitterCount, googleCount)
		if err := migratePublicFeedIndex(db, mdb, ndb, false); err != nil {
			log.Println("public feed index:", err)
		}
		ndb.Flush()
	case "public_feed":
		if err := migratePublicFeedIndex(db, mdb, ndb, true); err != nil {
			log.Fatal(err)
		}
		ndb.Flush()
	case "rebuild_timeline":
		options := timelineRebuildOptions{user: timelineUser, maxFeeds: timelineMaxFeeds, dryRun: dryRun}
		stats, err := rebuildTimelines(ndb, options)
		if err != nil {
			log.Fatal(err)
		}
		if !dryRun {
			ndb.Flush()
		}
		log.Printf("timeline summary: %d profiles, %d existing entries, %d follows, %d source entries, dry-run=%t", stats.profiles, stats.existing, stats.follows, stats.entries, dryRun)
	case "rebuild_social_graph":
		stats, err := rebuildSocialGraph(ndb, dryRun)
		if err != nil {
			log.Fatal(err)
		}
		if !dryRun {
			ndb.Flush()
		}
		log.Printf("social graph summary: %d feedinfos, %d edges, %d skipped references, dry-run=%t", stats.feedinfos, stats.edges, stats.skipped, dryRun)
	case "migrate_media_urls":
		stats, err := migrateMediaURLs(ndb, dryRun)
		if err != nil {
			log.Fatal(err)
		}
		if !dryRun {
			ndb.Flush()
		}
		log.Printf("media URL summary: %d profiles, %d entries, %d thumbnails, dry-run=%t", stats.profiles, stats.entries, stats.thumbnails, dryRun)
	case "sync":
		// EntryIndex
		model.EntryIndex.Iter(db, func(k, v []byte) error {
			return ndb.Set(k, v)
		})
		ndb.Flush()
		log.Println("iter done...")

		// Entry
		i := 0
		err := model.Entry.Iter(db, func(k, v []byte) error {
			i++
			return ndb.Set(k, v)
		})
		if err != nil {
			log.Println(err)
		}
		log.Println("synced entry count: ", i)
		ndb.Flush()
		log.Println("entry iter done...")
	case "profile":
		prefix := model.TableProfile
		j := 0
		n, err := mdb.ForwardScan(prefix.Bytes(), func(i int, k, v []byte) error {
			profile := &pb.Profile{}
			if err := proto.Unmarshal(v, profile); err != nil {
				return err
			}
			if profile.Id == "yinhm" {
				fmt.Printf("profile: %s\n", profile)

				new_value, _ := ndb.Get(k)
				// pb messge differ
				// if !bytes.Equal(v, new_value) {
				// 	fmt.Println(v)
				// 	fmt.Println(new_value)
				// 	return errors.New("value not equal")
				// }
				if err := proto.Unmarshal(new_value, profile); err != nil {
					return err
				}
				fmt.Printf("new profile: %s\n", profile)

				return nil
			}
			// fmt.Printf("profile: <%s, %s>\n", profile.Uuid, profile.Id)
			// fmt.Println(profile)

			j++
			return nil
		})
		if err != nil {
			fmt.Println("Error on scanning user profiles:", err)
		}
		fmt.Printf("Profiles: %d, %d have services.\n", n, j)

	case "purge_profile":
		n, err := purge_table(ndb, model.TableProfile.Bytes())
		if err != nil {
			fmt.Println("Error on scanning user profiles:", err)
		}
		fmt.Printf("Profiles: %d has been removed.\n", n)
		ndb.Flush()

	case "purge_oauth":
		n, err := purge_table(ndb, model.TableOAuth.Bytes())
		if err != nil {
			fmt.Println("Error on scanning oauth:", err)
		}
		fmt.Printf("oauth: %d has been removed.\n", n)
		ndb.Flush()

	case "count_meta":
		prefix := []byte("")
		log.Println("iter meta db now...")

		// prefix = model.TableProfile.Bytes()
		n, _ := mdb.ForwardScan(prefix, func(i int, k, v []byte) error {
			return nil
		})
		log.Printf("key counts: %d", n)

		// now test it
		// n, _ = mdb.ForwardScan(model.TableProfile.Bytes(), func(i int, k, v []byte) error {
		// 	return nil
		// })
		// count += n
		// n, _ = mdb.ForwardScan(model.TableOAuth.Bytes(), func(i int, k, v []byte) error {
		// 	return nil
		// })
		// count += n
		// log.Printf("key counts: %d", count)

		count := 0
		count_parsed := 0
		mdb.ForwardScan(prefix, func(i int, k, v []byte) error {
			msg := &pb.OAuthUser{}
			if err := proto.Unmarshal(v, msg); err == nil {
				count_parsed++
				// model.PutOAuthUser(ndb, msg) // oauth format updated
			} else {
				msg := &pb.Profile{}
				if err := proto.Unmarshal(v, msg); err == nil {
					count_parsed++
					// ndb.Set(k, v) // profile
				} else {
					count++
					uuid1, err := uuid.FromBytes(v)
					if err != nil {
						fmt.Println(k, v) // cache
					} else {
						v2, _ := model.UserMap.GetRaw(ndb, k)
						fmt.Println(string(k), uuid1.String(), bytes.Equal(v, v2))
						// update id->uuid map here
					}
				}
			}
			return nil
		})
		fmt.Printf("known keys: %d id->uuid map keys: %d\n", count_parsed, count)

	case "debug":
		// map changed, this will fail
		profile, err := model.GetProfileFromUserId(mdb, "yinhm")
		if err != nil {
			log.Println(err)
		}
		log.Println(profile)

		// profile, err = model.GetProfileFromUserId(mdb, "veronicabelmont")
		// if err != nil {
		// 	log.Println(err)
		// }
		// log.Println(profile)

		// entryId := "c8e5fe86df10e285f1ff12acac102d44"
		entryId := "744b310d5dca442d82f1ec2366554b1e"
		// entryId := "0000012820141462fa7290a658763ae1"
		entry, err := model.GetEntry(db, entryId)
		if err != nil {
			log.Println(err)
		}
		log.Println(entry)

		entry, err = model.GetEntry(ndb, entryId)
		if err != nil {
			log.Println(err)
		}
		log.Println(entry)

		// First entry
		// model.Entry.Iter(ndb, func(k, v []byte) error {
		// 	log.Println(model.Entry.ToStringKey(k))
		// 	log.Println(hex.EncodeToString(k), hex.EncodeToString(v))

		// 	return errors.New("stop iter")
		// })

		// test oauth user in new db
		_, msg, err := model.GetOAuthUser(ndb, "twitter", "5289142")
		if err != nil && err != model.ErrNotFound {
			log.Printf("oauth user not found: %s", err)
		}
		log.Printf("oauth user: <%s>", msg)

		model.OAuth.Iter(ndb, func(k, v []byte) error {
			log.Println(model.Entry.ToStringKey(k))
			log.Println(hex.EncodeToString(k), hex.EncodeToString(v))

			return errors.New("stop iter")
		})

		// uuidStr := "f82871b4-6b05-510a-9ae1-b626addf5b09"
		// profile, err := model.GetProfileFromUuid(db, uuidStr)
		// if err != nil {
		// 	log.Println(err)
		// }
		// log.Println(profile)

		v, _ := model.UserMap.GetRaw(ndb, []byte("yinhm"))
		log.Printf("id map: <%s>", v)
	}

	ndb.Close()
	if mdb != nil {
		mdb.Close()
	}
	if db != nil {
		db.Close()
	}
}
