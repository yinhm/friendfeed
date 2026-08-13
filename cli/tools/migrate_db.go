package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

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
var timelineMaxLimit int
var debugTable string
var inspectID string
var indexPath string
var dryRun bool

func init() {
	flag.StringVar(&fromPath, "from", "", "from directory")
	flag.StringVar(&toPath, "to", "", "to directory")
	flag.StringVar(&command, "c", "", "command to do")
	flag.StringVar(&timelineUser, "user", "", "limit timeline rebuild to one profile ID")
	flag.IntVar(&timelineMaxLimit, "max-limit", 0, "maximum source feeds per timeline / records per debug table dump (0 is unlimited)")
	flag.StringVar(&debugTable, "table", "", "debug: dump decoded records of the given table (oauth, profile)")
	flag.StringVar(&inspectID, "id", "", "profile or previous profile ID to inspect")
	flag.StringVar(&indexPath, "index-path", "", "search index directory (defaults to <to>/index, matching the server layout)")
	flag.BoolVar(&dryRun, "dry-run", false, "report supported migrations without writing changes")
}

func purge_table(db *store.Store, prefix store.Key) (int, error) {
	return db.ForwardScan(prefix, func(i int, k, v []byte) error {
		return db.Delete(k)
	})
}

// destructiveCommands 会不可逆地删除整表数据，执行前必须交互确认。
var destructiveCommands = map[string]bool{
	"purge_profile":         true,
	"purge_oauth":           true,
	"purge_user_rename_map": true,
}

// confirmDestructive 要求用户完整输入命令名才放行；脚本化场景可以管道喂入，
// 例如 `echo purge_profile | ./tools -to db -c purge_profile`。
func confirmDestructive(command, dbPath string, in io.Reader, out io.Writer) error {
	fmt.Fprintf(out, "WARNING: %q will permanently delete data in %s; this cannot be undone.\n", command, dbPath)
	fmt.Fprintf(out, "Type %q to continue: ", command)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.TrimSpace(line) != command {
		return errors.New("confirmation did not match; aborted")
	}
	return nil
}

type timelineRebuildStats struct {
	profiles  int
	follows   int
	entries   int
	existing  int
	mismatch  int
	duplicate int
}

type timelineRebuildOptions struct {
	user      string
	maxLimit  int
	dryRun    bool
	maxRows   int
	retention time.Duration
	now       time.Time
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

func timelineRebuildBounds(options timelineRebuildOptions) (int, time.Duration, time.Time) {
	maxRows := options.maxRows
	if maxRows <= 0 {
		maxRows = model.TimelineMaxEntries
	}
	retention := options.retention
	if retention <= 0 {
		retention = model.TimelineRetentionMax
	}
	now := options.now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return maxRows, retention, now
}

func rebuildTimelineForProfile(db *store.Store, profile *pb.Profile, options timelineRebuildOptions) (int, int, int, int, int, error) {
	profileID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("profile %q has invalid UUID: %w", profile.Id, err)
	}

	feeds := []uuid.UUID{profileID}
	seenFeeds := map[uuid.UUID]struct{}{profileID: {}}
	selectedFollows := 0
	followPrefix := model.NewKeyFrom(model.Follow.Prefix, profileID.Bytes())
	_, err = db.ForwardScan(followPrefix, func(i int, key, value []byte) error {
		if options.maxLimit > 0 && len(feeds) >= options.maxLimit {
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
		return 0, 0, 0, 0, 0, fmt.Errorf("scan follows for %s: %w", profile.Id, err)
	}

	maxRows, retention, now := timelineRebuildBounds(options)
	rebuilt, skippedInteractionDates, err := model.BuildHomeTimeline(db, feeds, maxRows, retention, now)
	if err != nil {
		return selectedFollows, 0, 0, 0, 0, fmt.Errorf("collect timeline candidates for %s: %w", profile.Id, err)
	}
	if skippedInteractionDates > 0 {
		log.Printf("timeline for %s: skipped %d interactions with missing or invalid dates", profile.Id, skippedInteractionDates)
	}
	existing, mismatches, duplicates, err := compareTimelineRows(db, profileID, rebuilt)
	if err != nil {
		return selectedFollows, len(rebuilt), existing, mismatches, duplicates, err
	}
	if !options.dryRun {
		if err := model.ReplaceHomeTimeline(db, profileID, rebuilt); err != nil {
			return selectedFollows, len(rebuilt), existing, mismatches, duplicates, fmt.Errorf("replace timeline for %s: %w", profile.Id, err)
		}
		if err := model.TouchTimelineState(db, profileID, now); err != nil {
			return selectedFollows, len(rebuilt), existing, mismatches, duplicates, fmt.Errorf("activate timeline for %s: %w", profile.Id, err)
		}
	}
	return selectedFollows, len(rebuilt), existing, mismatches, duplicates, nil
}

func compareTimelineRows(db *store.Store, viewer uuid.UUID, rebuilt map[uuid.UUID]time.Time) (existing, mismatches, duplicates int, err error) {
	for entry, activity := range rebuilt {
		old, getErr := model.TimelinePositionTime(db, viewer, entry)
		positionMatches := getErr == nil && old.Equal(activity)
		if getErr != nil && !errors.Is(getErr, store.ErrNotFound) {
			return 0, 0, 0, getErr
		}
		indexKey, keyErr := model.TimelineIndexKey(viewer, entry, activity)
		if keyErr != nil {
			return 0, 0, 0, keyErr
		}
		indexExists, existsErr := db.Exists(indexKey)
		if existsErr != nil {
			return 0, 0, 0, existsErr
		}
		if !positionMatches || !indexExists {
			mismatches++
		}
	}
	seenCandidates := make(map[uuid.UUID]struct{}, len(rebuilt))
	_, err = db.ForwardScan(model.TimelineIndexPrefix(viewer), func(_ int, key, _ []byte) error {
		existing++
		_, entry, _, parseErr := model.ParseTimelineIndexKey(key)
		if parseErr != nil {
			return parseErr
		}
		if _, expected := rebuilt[entry]; !expected {
			mismatches++
			return nil
		}
		if _, seen := seenCandidates[entry]; seen {
			duplicates++
			mismatches++
		} else {
			seenCandidates[entry] = struct{}{}
		}
		return nil
	})
	return existing, mismatches, duplicates, err
}

func rebuildTimelines(db *store.Store, options timelineRebuildOptions) (timelineRebuildStats, error) {
	stats := timelineRebuildStats{}
	profiles, oauthProfiles, err := oauthActiveProfiles(db)
	if err != nil {
		return stats, err
	}
	if len(oauthProfiles) == 0 && options.user == "" {
		return stats, errors.New("no profiles with OAuth information found")
	}

	for _, profile := range profiles {
		if options.user != "" && profile.Id != options.user {
			continue
		}
		if options.user != "" {
			log.Printf("explicit user %s selected; OAuth metadata check bypassed", profile.Id)
		} else {
			profileID, err := uuid.FromString(profile.Uuid)
			if err != nil {
				return stats, fmt.Errorf("profile %q has invalid UUID: %w", profile.Id, err)
			}
			if _, active := oauthProfiles[profileID]; !active {
				continue
			}
		}

		follows, entries, existing, mismatches, duplicates, err := rebuildTimelineForProfile(db, profile, options)
		if err != nil {
			return stats, err
		}
		stats.profiles++
		stats.follows += follows
		stats.entries += entries
		stats.existing += existing
		stats.mismatch += mismatches
		stats.duplicate += duplicates
		action := "rebuilt"
		if options.dryRun {
			action = "would rebuild"
		}
		log.Printf("%s timeline for %s: %d existing entries, %d source feeds, %d source entries, %d mismatches, %d duplicates", action, profile.Id, existing, follows+1, entries, mismatches, duplicates)
	}
	if options.user != "" && stats.profiles == 0 {
		return stats, fmt.Errorf("profile %q not found", options.user)
	}
	return stats, nil
}

func runDBCommand(db, ndb *store.Store) {
	prefix := []byte("")

	log.Println("iter db now...")

	// iter here
	iter, err := db.NewIterator(prefix)
	if err != nil {
		log.Fatalf("create database iterator: %v", err)
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		if err := ndb.Set(iter.Key(), iter.Value()); err != nil {
			log.Fatalf("copy database key %x: %v", iter.UnsafeRawKey(), err)
		}
	}
	if err := iter.Error(); err != nil {
		log.Fatalf("iterate database: %v", err)
	}
	if err := ndb.Flush(); err != nil {
		log.Fatalf("flush database: %v", err)
	}
	log.Println("iter done...")
}

func runRebuildTimelineCommand(ndb *store.Store) {
	options := timelineRebuildOptions{user: timelineUser, maxLimit: timelineMaxLimit, dryRun: dryRun}
	stats, err := rebuildTimelines(ndb, options)
	if err != nil {
		log.Fatal(err)
	}
	if !dryRun {
		if err := ndb.Flush(); err != nil {
			log.Fatalf("flush database: %v", err)
		}
	}
	log.Printf("timeline summary: %d profiles, %d existing entries, %d follows, %d source entries, %d mismatches, %d duplicates, dry-run=%t", stats.profiles, stats.existing, stats.follows, stats.entries, stats.mismatch, stats.duplicate, dryRun)
}

func runRebuildSocialGraphCommand(ndb *store.Store) {
	stats, err := rebuildSocialGraph(ndb, dryRun)
	if err != nil {
		log.Fatal(err)
	}
	if !dryRun {
		if err := ndb.Flush(); err != nil {
			log.Fatalf("flush database: %v", err)
		}
	}
	log.Printf("social graph summary: %d feedinfos, %d edges, %d skipped references, dry-run=%t", stats.feedinfos, stats.edges, stats.skipped, dryRun)
}

func runMigrateMediaURLsCommand(ndb *store.Store) {
	stats, err := migrateMediaURLs(ndb, dryRun)
	if err != nil {
		log.Fatal(err)
	}
	if !dryRun {
		if err := ndb.Flush(); err != nil {
			log.Fatalf("flush database: %v", err)
		}
	}
	log.Printf("media URL summary: %d profiles, %d entries, %d thumbnails, dry-run=%t", stats.profiles, stats.entries, stats.thumbnails, dryRun)
}

func runBackfillActorUUIDsCommand(ndb *store.Store) {
	options := actorUUIDBackfillOptions{
		user:     timelineUser,
		maxLimit: timelineMaxLimit,
		dryRun:   dryRun,
	}
	stats, err := backfillActorUUIDs(ndb, options)
	if err != nil {
		log.Fatal(err)
	}
	if !dryRun {
		// Writes have already committed to Pebble's WAL/memtable. Flush only
		// forces the memtable to stable storage; it is not the dry-run switch.
		if err := ndb.Flush(); err != nil {
			log.Fatalf("flush database: %v", err)
		}
	}
	log.Printf(
		"actor UUID backfill summary: %d entries scanned, %d entries changed, %d entry authors, %d comments, %d likes, %d already set, %d unresolved, %d conflicts, dry-run=%t",
		stats.entriesScanned,
		stats.entriesChanged,
		stats.entryAuthors,
		stats.comments,
		stats.likes,
		stats.alreadySet,
		stats.unresolved,
		stats.conflicts,
		dryRun,
	)
}

func runSyncCommand(db, ndb *store.Store) {
	// EntryIndex
	model.EntryIndex.Iter(db, func(k, v []byte) error {
		return ndb.Set(k, v)
	})
	if err := ndb.Flush(); err != nil {
		log.Fatalf("flush database: %v", err)
	}
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
	if err := ndb.Flush(); err != nil {
		log.Fatalf("flush database: %v", err)
	}
	log.Println("entry iter done...")
}

func runPurgeProfileCommand(ndb *store.Store) {
	n, err := purge_table(ndb, model.TableProfile.Bytes())
	if err != nil {
		fmt.Println("Error on scanning user profiles:", err)
	}
	fmt.Printf("Profiles: %d has been removed.\n", n)
	if err := ndb.Flush(); err != nil {
		log.Fatalf("flush database: %v", err)
	}
}

func runPurgeOAuthCommand(ndb *store.Store) {
	n, err := purge_table(ndb, model.TableOAuth.Bytes())
	if err != nil {
		fmt.Println("Error on scanning oauth:", err)
	}
	fmt.Printf("oauth: %d has been removed.\n", n)
	if err := ndb.Flush(); err != nil {
		log.Fatalf("flush database: %v", err)
	}
}

func inspectUserRenameMap(db *store.Store, oldID string, maxLimit int, out io.Writer) (int, error) {
	n := 0
	printRecord := func(id string, raw []byte) error {
		profileUUID, err := uuid.FromBytes(raw)
		if err != nil {
			return fmt.Errorf("decode UserRenameMap[%s]: %w", id, err)
		}
		profile, err := model.GetProfileFromUuid(db, profileUUID)
		if err != nil {
			return fmt.Errorf("resolve UserRenameMap[%s] profile %s: %w", id, profileUUID, err)
		}
		fmt.Fprintf(out, "%s -> %s -> %s\n", id, profileUUID, profile.Id)
		n++
		return nil
	}

	if oldID != "" {
		raw, err := model.UserRenameMap.GetRaw(db, []byte(oldID))
		if err != nil {
			return 0, err
		}
		if len(raw) == 0 {
			return 0, model.ErrNotFound
		}
		if err := printRecord(oldID, raw); err != nil {
			return 0, err
		}
		return n, nil
	}

	err := model.UserRenameMap.Iter(db, func(key, raw []byte) error {
		if maxLimit > 0 && n >= maxLimit {
			return errDebugLimitReached
		}
		oldID := string(model.UserRenameMap.PrefixRemove(store.Key(key)))
		return printRecord(oldID, raw)
	})
	if errors.Is(err, errDebugLimitReached) {
		err = nil
	}
	return n, err
}

func runInspectUserRenameMapCommand(ndb *store.Store) {
	n, err := inspectUserRenameMap(ndb, inspectID, timelineMaxLimit, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("UserRenameMap: %d records inspected (max-limit=%d)", n, timelineMaxLimit)
}

func runPurgeUserRenameMapCommand(ndb *store.Store) {
	n, err := purge_table(ndb, model.TableUserRenameMap.Bytes())
	if err != nil {
		log.Fatalf("purge UserRenameMap: %v", err)
	}
	fmt.Printf("UserRenameMap: %d records removed.\n", n)
}

// runFixTwitterOAuthFieldsCommand swaps Name and NickName on every twitter
// OAuth row. Migrated rows use the old field order (Name=display,
// NickName=handle); the current login path (httpd/src/auth.go) expects
// Name=handle, NickName=display. Only touches provider == "twitter".
func runFixTwitterOAuthFieldsCommand(ndb *store.Store) {
	n := 0
	err := model.OAuth.Iter(ndb, func(key, raw []byte) error {
		u := new(pb.OAuthUser)
		if err := proto.Unmarshal(raw, u); err != nil {
			return fmt.Errorf("decode oauth at %x: %w", key, err)
		}
		if strings.ToLower(u.Provider) != "twitter" {
			return nil
		}
		u.Name, u.NickName = u.NickName, u.Name
		// Iter's key buffer is reused across iterations; strip the table
		// prefix and copy before writing back to the same slot.
		k := append(store.Key(nil), model.OAuth.PrefixRemove(store.Key(key))...)
		if _, err := model.OAuth.Put(ndb, k, u); err != nil {
			return fmt.Errorf("write oauth twitter:%s: %w", u.UserId, err)
		}
		n++
		return nil
	})
	if err != nil {
		log.Fatalf("fix twitter oauth fields: %v", err)
	}
	if err := ndb.Flush(); err != nil {
		log.Fatalf("flush database: %v", err)
	}
	fmt.Printf("swapped Name/NickName on %d twitter oauth rows\n", n)
}

var errDebugLimitReached = errors.New("debug table limit reached")

// dumpTable 逐条解码并打印表记录；maxLimit 为 0 时不限条数。
// keyFn 负责把内部 key 渲染成可读形式（不同表 key 编码不同）。
func dumpTable(db *store.Store, table *model.Table, newMsg func() proto.Message, keyFn func(key []byte) string, maxLimit int, out io.Writer) (int, error) {
	n := 0
	err := table.Iter(db, func(key, raw []byte) error {
		if maxLimit > 0 && n >= maxLimit {
			return errDebugLimitReached
		}
		msg := newMsg()
		if err := proto.Unmarshal(raw, msg); err != nil {
			return fmt.Errorf("decode record at %x: %w", key, err)
		}
		fmt.Fprintf(out, "%s: %v\n", keyFn(key), msg)
		n++
		return nil
	})
	if errors.Is(err, errDebugLimitReached) {
		err = nil
	}
	return n, err
}

// stripPrefixKey 去掉表前缀后按字符串渲染 key，适用于字符串编码的表（如 oauth）。
func stripPrefixKey(table *model.Table, key []byte) string {
	return strings.TrimPrefix(string(key), string(table.Prefix))
}

func runDebugTableCommand(ndb *store.Store, tableName string, maxLimit int) {
	var table *model.Table
	var newMsg func() proto.Message
	var keyFn func(key []byte) string
	switch tableName {
	case "oauth":
		table = model.OAuth
		newMsg = func() proto.Message { return new(pb.OAuthUser) }
		keyFn = func(key []byte) string { return stripPrefixKey(table, key) }
	case "profile":
		table = model.Profile
		newMsg = func() proto.Message { return new(pb.Profile) }
		keyFn = func(key []byte) string { return table.ToStringKey(store.Key(key)) }
	default:
		log.Fatalf("unknown debug table %q (supported: oauth, profile)", tableName)
	}

	n, err := dumpTable(ndb, table, newMsg, keyFn, maxLimit, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("debug table %s: %d records dumped (max-limit=%d)", tableName, n, maxLimit)
}

// runInspectProfileCommand traces the two-step profile lookup used by the
// feed handler: UserMap (login id -> uuid) and Profile (uuid -> record). A
// feed 404 means one of these links is broken, so we report each hop and the
// raw bytes involved rather than a single opaque "not found".
func runInspectProfileCommand(ndb *store.Store, id string) {
	if id == "" {
		log.Fatal("-id is required for inspect_profile")
	}
	fmt.Printf("inspecting login id %q\n", id)

	mapKey := model.NewKeyFrom(model.TableUserMap.Bytes(), []byte(id))
	raw, err := ndb.Get(mapKey)
	if errors.Is(err, store.ErrNotFound) {
		fmt.Printf("  UserMap: MISSING (no %q -> uuid mapping)\n", id)
	} else if err != nil {
		log.Fatalf("UserMap get %q: %v", id, err)
	} else {
		fmt.Printf("  UserMap: %q -> uuid bytes %x (len=%d)\n", id, raw, len(raw))
		profileUUID, err := uuid.FromBytes(raw)
		if err != nil {
			fmt.Printf("  UserMap value is not a valid uuid: %v\n", err)
		} else {
			fmt.Printf("  uuid: %s\n", profileUUID)
			inspectProfileByUUID(ndb, profileUUID)
		}
	}

	// Reverse check: scan the Profile table for a record whose Id matches, so
	// we can tell a missing UserMap entry apart from a missing Profile record.
	fmt.Printf("  scanning Profile table for Id == %q ...\n", id)
	found := 0
	err = model.Profile.Iter(ndb, func(key, rawval []byte) error {
		p := new(pb.Profile)
		if err := proto.Unmarshal(rawval, p); err != nil {
			return fmt.Errorf("decode profile at %x: %w", key, err)
		}
		if p.Id == id {
			found++
			fmt.Printf("    match: profile key uuid=%s, Uuid field=%q, deleted=%t\n",
				model.Profile.ToStringKey(store.Key(key)), p.Uuid, p.Deleted)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("scan Profile: %v", err)
	}
	fmt.Printf("  Profile scan matches: %d\n", found)

	// The OAuth row is the source of truth for a login. If it carries a Uuid we
	// can tell whether the Profile/UserMap rows should have been derived from it.
	fmt.Printf("  scanning OAuth table for Name/NickName/UserId == %q ...\n", id)
	oauthHits := 0
	err = model.OAuth.Iter(ndb, func(key, rawval []byte) error {
		u := new(pb.OAuthUser)
		if err := proto.Unmarshal(rawval, u); err != nil {
			return fmt.Errorf("decode oauth at %x: %w", key, err)
		}
		if u.Name == id || u.NickName == id || u.UserId == id {
			oauthHits++
			fmt.Printf("    match: key=%q provider=%q userId=%q name=%q nickName=%q uuid=%q\n",
				stripPrefixKey(model.OAuth, key), u.Provider, u.UserId, u.Name, u.NickName, u.Uuid)
			if u.Uuid != "" {
				if pu, err := uuid.FromString(u.Uuid); err == nil {
					inspectProfileByUUID(ndb, pu)
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Fatalf("scan OAuth: %v", err)
	}
	fmt.Printf("  OAuth scan matches: %d\n", oauthHits)
}

func inspectProfileByUUID(db *store.Store, profileUUID uuid.UUID) {
	msg := new(pb.Profile)
	err := model.Profile.Get(db, profileUUID.Bytes(), msg)
	if errors.Is(err, model.ErrNotFound) {
		fmt.Printf("  Profile: MISSING for uuid %s\n", profileUUID)
		return
	}
	if err != nil {
		fmt.Printf("  Profile get error: %v\n", err)
		return
	}
	fmt.Printf("  Profile: found Id=%q Name=%q Type=%q deleted=%t\n", msg.Id, msg.Name, msg.Type, msg.Deleted)
}

// runAuditProfilesCommand walks every OAuth login and checks that its Twitter
// handle (nickName) resolves to a feed via the UserMap. It classifies each
// mismatch so we can tell a migration bug (handle normalized/truncated into
// Profile.Id) apart from a legitimate FriendFeed-vs-Twitter name difference.
func runAuditProfilesCommand(ndb *store.Store) {
	total, resolvable, handleMismatch, unresolvable := 0, 0, 0, 0
	err := model.OAuth.Iter(ndb, func(key, raw []byte) error {
		u := new(pb.OAuthUser)
		if err := proto.Unmarshal(raw, u); err != nil {
			return fmt.Errorf("decode oauth at %x: %w", key, err)
		}
		total++
		handle := u.NickName // gothic screen_name lives in NickName
		if handle == "" {
			return nil
		}
		// Does the Twitter handle resolve to a feed directly?
		mapKey := model.NewKeyFrom(model.TableUserMap.Bytes(), []byte(handle))
		_, err := ndb.Get(mapKey)
		if err == nil {
			resolvable++
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("read UserMap[%s]: %w", handle, err)
		}
		// Handle does not resolve. Look up the profile via the OAuth uuid to
		// see what Id it actually carries.
		profileID := "<none>"
		if u.Uuid != "" {
			if pu, err := uuid.FromString(u.Uuid); err == nil {
				p := new(pb.Profile)
				if err := model.Profile.Get(ndb, pu.Bytes(), p); err == nil {
					profileID = p.Id
				}
			}
		}
		if profileID != "<none>" && profileID != handle {
			handleMismatch++
			fmt.Printf("mismatch: handle=%q profileId=%q userId=%q uuid=%q\n",
				handle, profileID, u.UserId, u.Uuid)
		} else {
			unresolvable++
			fmt.Printf("unresolvable: handle=%q profileId=%q userId=%q uuid=%q\n",
				handle, profileID, u.UserId, u.Uuid)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("audit profiles: %v", err)
	}
	fmt.Printf("\naudit summary: %d oauth logins, %d resolvable, %d handle!=profileId, %d unresolvable\n",
		total, resolvable, handleMismatch, unresolvable)

	// The feed-routing invariant: every non-deleted Profile.Id must resolve
	// through UserMap back to that same profile uuid. A break here is what
	// makes /feed/<id> return 404 for a profile that plainly exists.
	profiles, mapMissing, mapMismatch, badUUID := 0, 0, 0, 0
	err = model.Profile.Iter(ndb, func(key, raw []byte) error {
		p := new(pb.Profile)
		if err := proto.Unmarshal(raw, p); err != nil {
			return fmt.Errorf("decode profile at %x: %w", key, err)
		}
		if p.Deleted || p.Id == "" {
			return nil
		}
		profiles++
		pu, err := uuid.FromString(p.Uuid)
		if err != nil {
			badUUID++
			fmt.Printf("bad profile uuid: id=%q uuid=%q\n", p.Id, p.Uuid)
			return nil
		}
		mapKey := model.NewKeyFrom(model.TableUserMap.Bytes(), []byte(p.Id))
		v, err := ndb.Get(mapKey)
		if errors.Is(err, store.ErrNotFound) {
			mapMissing++
			fmt.Printf("usermap MISSING: id=%q uuid=%q\n", p.Id, p.Uuid)
			return nil
		}
		if err != nil {
			return fmt.Errorf("read UserMap[%s]: %w", p.Id, err)
		}
		mapped, err := uuid.FromBytes(v)
		if err != nil || mapped != pu {
			mapMismatch++
			fmt.Printf("usermap MISMATCH: id=%q profileUuid=%q mappedBytes=%x\n", p.Id, p.Uuid, v)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("audit profile->usermap: %v", err)
	}
	fmt.Printf("\nprofile routing: %d profiles, %d usermap missing, %d usermap mismatch, %d bad uuid\n",
		profiles, mapMissing, mapMismatch, badUUID)

	// Feasibility of aliasing handle -> feed: would adding UserMap[handle]
	// collide with a *different* profile's existing id?
	aliasable, aliasCollision, aliasSameOwner := 0, 0, 0
	err = model.OAuth.Iter(ndb, func(key, raw []byte) error {
		u := new(pb.OAuthUser)
		if err := proto.Unmarshal(raw, u); err != nil {
			return err
		}
		handle := u.NickName
		if handle == "" || u.Uuid == "" {
			return nil
		}
		pu, err := uuid.FromString(u.Uuid)
		if err != nil {
			return nil
		}
		mapKey := model.NewKeyFrom(model.TableUserMap.Bytes(), []byte(handle))
		v, err := ndb.Get(mapKey)
		if errors.Is(err, store.ErrNotFound) {
			aliasable++ // handle is free; safe to alias to this uuid
			return nil
		}
		if err != nil {
			return fmt.Errorf("read UserMap[%s]: %w", handle, err)
		}
		if mapped, err := uuid.FromBytes(v); err == nil && mapped == pu {
			aliasSameOwner++ // already points at the same owner (no-op)
		} else {
			aliasCollision++ // handle taken by a different profile
			fmt.Printf("alias collision: handle=%q wanted uuid=%q but usermap has %x\n", handle, u.Uuid, v)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("audit alias feasibility: %v", err)
	}
	fmt.Printf("alias feasibility: %d free-to-alias, %d already-owner, %d collide with other profile\n",
		aliasable, aliasSameOwner, aliasCollision)
}

func runDebugCommand(db, ndb *store.Store) {
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
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		log.Printf("oauth user not found: %s", err)
	}
	if msg != nil {
		log.Printf("oauth user: provider=%s user_id=%s uuid=%s", msg.Provider, msg.UserId, msg.Uuid)
	}

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

// We should open original db ad readonly mode.
//
// rebuild Home timelines from each profile's own feed and Follow edges
// ./tools -to new_db -c rebuild_timeline -user yinhm -max-limit 20 -dry-run
// rebuild Follow and Follower tables from legacy Feedinfo metadata
// ./tools -to new_db -c rebuild_social_graph -dry-run
// migrate legacy GCS and FriendFeed media URLs in profiles and entries
// ./tools -to new_db -c migrate_media_urls -dry-run
// backfill stable actor UUIDs
// ./tools -to new_db -c backfill_actor_uuids -user yinhm -max-limit 20 -dry-run
// dump decoded table records
// ./tools -to new_db -c debug -table oauth -max-limit 10
//
// sync all data from db
// ./tools -from old_db -to new_db -c db
//
// debug
// ./tools -from old_db -to new_db -c debug
func main() {
	flag.Parse()

	if toPath == "" {
		log.Fatal("-to is required")
	}
	// Only a few commands read from a source db; everything else operates on
	// the target (-to) alone. Default to not requiring -from and opt those in.
	needsSource := command == "db" || command == "sync" ||
		(command == "debug" && debugTable == "")
	// readOnly commands only inspect the target db; open it read-only so we
	// never mutate on-disk state or fight another process for the write lock.
	readOnly := command == "inspect_profile" || command == "inspect_user_rename_map" ||
		command == "audit_profiles" || command == "audit_store" ||
		command == "rebuild_search_index" ||
		(command == "debug" && debugTable != "") ||
		(command == "migrate_entry_index" && dryRun) ||
		(command == "migrate_entry_keys" && dryRun) ||
		(command == "migrate_interactions" && dryRun) ||
		(command == "rebuild_entry_index" && dryRun) ||
		(command == "backfill_actor_uuids" && dryRun)
	if command == "compact_timelines" && dryRun {
		readOnly = true
	}
	if needsSource && fromPath == "" {
		log.Fatal("-from is required for command ", command)
	}

	// 确认必须发生在打开(创建)目标库之前,避免误操作产生副作用。
	if destructiveCommands[command] {
		if err := confirmDestructive(command, toPath, os.Stdin, os.Stderr); err != nil {
			log.Fatal(err)
		}
	}

	var ndb *store.Store
	var err error
	if readOnly {
		ndb, err = store.NewStoreReadOnly(toPath)
	} else {
		ndb, err = store.NewStore(toPath)
	}
	if err != nil {
		log.Fatalf("open target database %s: %v", toPath, err)
	}
	if !readOnly {
		ndb.SetSync(false)
	}
	var db *store.Store
	if needsSource {
		db, err = store.NewStore(fromPath)
		if err != nil {
			log.Fatalf("open source database %s: %v", fromPath, err)
		}
	}

	switch command {
	case "db":
		runDBCommand(db, ndb)
	case "rebuild_timeline":
		runRebuildTimelineCommand(ndb)
	case "compact_timelines":
		stats, err := compactTimelines(ndb, timelineCompactOptions{
			user: timelineUser, dryRun: dryRun,
			maxRows: model.TimelineMaxEntries, coldRows: model.TimelineColdEntries,
			retention: model.TimelineRetentionMax,
		})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("timeline compaction: viewers=%d inactive=%d indexes=%d positions=%d deleted_indexes=%d deleted_positions=%d dry-run=%t",
			stats.viewers, stats.inactiveViewers, stats.indexes, stats.positions,
			stats.deletedIndexes, stats.deletedPositions, dryRun)
	case "rebuild_social_graph":
		runRebuildSocialGraphCommand(ndb)
	case "migrate_media_urls":
		runMigrateMediaURLsCommand(ndb)
	case "backfill_actor_uuids":
		runBackfillActorUUIDsCommand(ndb)
	case "rebuild_search_index":
		if indexPath == "" {
			indexPath = filepath.Join(toPath, "index")
		}
		runRebuildSearchIndexCommand(ndb, indexPath)
	case "sync":
		runSyncCommand(db, ndb)
	case "purge_profile":
		runPurgeProfileCommand(ndb)
	case "purge_oauth":
		runPurgeOAuthCommand(ndb)
	case "purge_user_rename_map":
		runPurgeUserRenameMapCommand(ndb)
	case "inspect_profile":
		runInspectProfileCommand(ndb, inspectID)
	case "inspect_user_rename_map":
		runInspectUserRenameMapCommand(ndb)
	case "audit_profiles":
		runAuditProfilesCommand(ndb)
	case "audit_store":
		stats, err := auditStore(ndb)
		if err != nil {
			log.Fatal(err)
		}
		writeStoreAudit(os.Stdout, stats)
	case "migrate_entry_index":
		stats, err := migrateEntryIndex(ndb, dryRun, timelineMaxLimit)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("entry index migration: scanned=%d migrated=%d current=%d dry-run=%t",
			stats.scanned, stats.migrated, stats.current, dryRun)
	case "migrate_entry_keys":
		stats, err := migrateEntryKeys(ndb, dryRun)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("entry key migration: scanned=%d canonical=%d migrated=%d dry-run=%t",
			stats.scanned, stats.canonical, stats.migrated, dryRun)
	case "migrate_interactions":
		stats, err := migrateInteractions(ndb, interactionMigrationOptions{
			user: timelineUser, maxLimit: timelineMaxLimit, dryRun: dryRun,
		})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("interaction migration: scanned=%d migrated=%d likes=%d comments=%d legacy_actors=%d generated_comment_ids=%d duplicates=%d dry-run=%t",
			stats.entriesScanned, stats.entriesMigrated, stats.likes, stats.comments,
			stats.legacyActors, stats.generatedIDs, stats.duplicates, dryRun)
	case "rebuild_entry_index":
		stats, err := rebuildEntryIndexes(ndb, entryIndexRebuildOptions{
			user: timelineUser, maxLimit: timelineMaxLimit, dryRun: dryRun,
		})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("entry index rebuild: entries=%d direct=%d removed=%d feeds_checked=%d feeds_mismatched=%d duplicate_indexes=%d dry-run=%t",
			stats.entries, stats.direct, stats.removed, stats.feedsChecked,
			stats.feedsMismatched, stats.duplicateIndexes, dryRun)
	case "fix_twitter_oauth_fields":
		runFixTwitterOAuthFieldsCommand(ndb)
	case "debug":
		if debugTable != "" {
			runDebugTableCommand(ndb, debugTable, timelineMaxLimit)
		} else {
			runDebugCommand(db, ndb)
		}
	default:
		// Retired old_db migration commands (meta, sync_meta, public_feed,
		// profile, count_meta) must fail loudly rather than exit 0 silently.
		log.Fatalf("unknown command %q", command)
	}

	ndb.Close()
	if db != nil {
		db.Close()
	}
}
