package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func TestConfirmDestructive(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "exact command name", input: "purge_profile\n"},
		{name: "surrounding whitespace tolerated", input: "  purge_profile  \n"},
		{name: "piped input without newline", input: "purge_profile"},
		{name: "plain yes is not enough", input: "yes\n", wantErr: true},
		{name: "wrong command name", input: "purge_oauth\n", wantErr: true},
		{name: "empty input", input: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := new(bytes.Buffer)
			err := confirmDestructive("purge_profile", "/data/db", strings.NewReader(tt.input), out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("confirmDestructive(%q) err = %v; wantErr %t", tt.input, err, tt.wantErr)
			}
			if !strings.Contains(out.String(), "WARNING") {
				t.Fatalf("prompt missing warning: %q", out.String())
			}
		})
	}
}

func TestInspectAndPurgeUserRenameMap(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   "before",
		Name: "Rename",
		Type: "user",
	}); err != nil {
		t.Fatal(err)
	}
	if err := model.RenameProfileId(db, profileUUID, "after"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	n, err := inspectUserRenameMap(db, "before", 0, &out)
	if err != nil {
		t.Fatal(err)
	}
	want := "before -> " + profileUUID.String() + " -> after\n"
	if n != 1 || out.String() != want {
		t.Fatalf("inspect = (%d, %q); want (1, %q)", n, out.String(), want)
	}

	removed, err := purge_table(db, model.TableUserRenameMap.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("purged %d records; want 1", removed)
	}
	if _, err := model.FindProfileRenameByOldId(db, "before"); err == nil {
		t.Fatal("purged rename record is still resolvable")
	}
	if err := model.RenameProfileId(db, profileUUID, "final"); err != nil {
		t.Fatalf("rename after reclamation: %v", err)
	}
}

func TestUserRenameMapPurgeRequiresConfirmation(t *testing.T) {
	if !destructiveCommands["purge_user_rename_map"] {
		t.Fatal("purge_user_rename_map is not marked destructive")
	}
}

func TestFixTwitterOAuthFields(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	seed := []*pb.OAuthUser{
		{Provider: "twitter", UserId: "1", Name: "芸窗", NickName: "yun_chuang"},
		{Provider: "twitter", UserId: "2", Name: "Olive Fee", NickName: "Olivefee"},
		// non-twitter rows must never be touched
		{Provider: "google", UserId: "3", Name: "Yin Heming", NickName: "epaulin"},
	}
	for _, u := range seed {
		if _, err := model.PutOAuthUser(db, u); err != nil {
			t.Fatalf("seed %s: %v", u.UserId, err)
		}
	}

	runFixTwitterOAuthFieldsCommand(db)

	want := map[string]struct{ name, nick string }{
		"1": {"yun_chuang", "芸窗"},
		"2": {"Olivefee", "Olive Fee"},
	}
	for id, w := range want {
		_, u, err := model.GetOAuthUser(db, "twitter", id)
		if err != nil {
			t.Fatalf("get twitter:%s: %v", id, err)
		}
		if u.Name != w.name || u.NickName != w.nick {
			t.Fatalf("twitter:%s = (Name=%q, NickName=%q); want (Name=%q, NickName=%q)",
				id, u.Name, u.NickName, w.name, w.nick)
		}
	}

	// google row must be untouched
	if _, u, _ := model.GetOAuthUser(db, "google", "3"); u.Name != "Yin Heming" || u.NickName != "epaulin" {
		t.Fatalf("google row mutated: Name=%q NickName=%q", u.Name, u.NickName)
	}
}

func TestDumpTable(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	for _, userID := range []string{"dump-1", "dump-2", "dump-3"} {
		_, err := model.PutOAuthUser(db, &pb.OAuthUser{
			UserId:   userID,
			Provider: "twitter",
		})
		if err != nil {
			t.Fatalf("PutOAuthUser(%q): %v", userID, err)
		}
	}

	newMsg := func() proto.Message { return new(pb.OAuthUser) }
	keyFn := func(key []byte) string { return stripPrefixKey(model.OAuth, key) }

	// unlimited: all records, decoded and printed with string keys
	out := new(bytes.Buffer)
	n, err := dumpTable(db, model.OAuth, newMsg, keyFn, 0, out)
	if err != nil {
		t.Fatalf("dumpTable: %v", err)
	}
	if n != 3 {
		t.Fatalf("dumped %d records; want 3", n)
	}
	for _, want := range []string{"twitter:dump-1", "twitter:dump-2", "twitter:dump-3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %q", want, out.String())
		}
	}

	// max-limit caps the dump
	out.Reset()
	n, err = dumpTable(db, model.OAuth, newMsg, keyFn, 2, out)
	if err != nil {
		t.Fatalf("dumpTable with limit: %v", err)
	}
	if n != 2 {
		t.Fatalf("dumped %d records; want 2", n)
	}
	if got := strings.Count(out.String(), "twitter:dump-"); got != 2 {
		t.Fatalf("output has %d records; want 2: %q", got, out.String())
	}
}

func TestDumpTableProfile(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	profileID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: profileID.String(), Id: "yinhm", Type: "user"}
	if err := model.UpdateProfile(db, profile); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	newMsg := func() proto.Message { return new(pb.Profile) }
	keyFn := func(key []byte) string { return model.Profile.ToStringKey(store.Key(key)) }

	out := new(bytes.Buffer)
	n, err := dumpTable(db, model.Profile, newMsg, keyFn, 0, out)
	if err != nil {
		t.Fatalf("dumpTable: %v", err)
	}
	if n != 1 {
		t.Fatalf("dumped %d records; want 1", n)
	}
	// Profile keys render as the hex-encoded UUID; the value must be decoded.
	for _, want := range []string{fmt.Sprintf("%x", profileID.Bytes()), "yinhm"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %q", want, out.String())
		}
	}
}

func TestMigrateMediaURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "GCS profile image",
			in:   "https://storage.googleapis.com/lastff01/p-c6f8dca854f011ddb489003048343a40-large-1002",
			want: "https://m.friendfeed.me/p-c6f8dca854f011ddb489003048343a40-large-1002",
			ok:   true,
		},
		{
			name: "legacy thumbnail host",
			in:   "http://m.friendfeed-media.com/b4c16ec30ea16e9cf98d138cd274a8676f1e3b96",
			want: "http://m.friendfeed.me/b4c16ec30ea16e9cf98d138cd274a8676f1e3b96",
			ok:   true,
		},
		{name: "external image", in: "https://example.com/image.jpg", want: "https://example.com/image.jpg"},
		{name: "already migrated", in: "https://m.friendfeed.me/image.jpg", want: "https://m.friendfeed.me/image.jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := migrateMediaURL(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("migrateMediaURL(%q) = %q, %t; want %q, %t", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestMigrateMediaURLsOnlyUpdatesNewDatabase(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	db.SetSync(false)

	profileID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{
		Uuid:    profileID.String(),
		Id:      "user",
		Picture: "https://storage.googleapis.com/lastff01/p-avatar-large",
	}
	if err := model.UpdateProfile(db, profile); err != nil {
		t.Fatal(err)
	}
	entryID := uuid.Must(uuid.NewV4())
	entry := &pb.Entry{
		Id: entryID.String(),
		Thumbnails: []*pb.Thumbnail{
			{Url: "http://m.friendfeed-media.com/thumbnail"},
			{Url: "https://example.com/external.jpg"},
		},
	}
	if _, err := model.Entry.Put(db, entryID.Bytes(), entry); err != nil {
		t.Fatal(err)
	}

	dryStats, err := migrateMediaURLs(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if dryStats.profiles != 1 || dryStats.entries != 1 || dryStats.thumbnails != 1 {
		t.Fatalf("unexpected dry-run stats: %+v", dryStats)
	}
	unchanged, err := model.GetProfileFromUuid(db, profileID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Picture != profile.Picture {
		t.Fatalf("dry-run changed profile picture to %q", unchanged.Picture)
	}

	stats, err := migrateMediaURLs(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if stats != dryStats {
		t.Fatalf("migration stats %+v differ from dry-run %+v", stats, dryStats)
	}
	migratedProfile, err := model.GetProfileFromUuid(db, profileID)
	if err != nil {
		t.Fatal(err)
	}
	if migratedProfile.Picture != "https://m.friendfeed.me/p-avatar-large" {
		t.Fatalf("unexpected profile picture: %q", migratedProfile.Picture)
	}
	migratedEntry, err := model.GetEntry(db, entryID.String())
	if err != nil {
		t.Fatal(err)
	}
	if got := migratedEntry.Thumbnails[0].Url; got != "http://m.friendfeed.me/thumbnail" {
		t.Fatalf("unexpected thumbnail URL: %q", got)
	}
	if got := migratedEntry.Thumbnails[1].Url; got != "https://example.com/external.jpg" {
		t.Fatalf("external thumbnail was changed: %q", got)
	}
}

func TestRebuildTimelines(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	db.SetSync(false)

	userID := uuid.Must(uuid.NewV4())
	followedID := uuid.Must(uuid.NewV4())
	for id, name := range map[uuid.UUID]string{userID: "user", followedID: "followed"} {
		if err := model.UpdateProfile(db, &pb.Profile{Uuid: id.String(), Id: name, Type: "user"}); err != nil {
			t.Fatal(err)
		}
	}
	// Legacy OAuth rows can be bound by login name without carrying UUID.
	if _, err := model.OAuth.Put(db, model.KeyFromString("twitter", "active-user"), &pb.OAuthUser{
		Name: "user", Provider: "twitter", UserId: "active-user",
	}); err != nil {
		t.Fatal(err)
	}

	followKey := model.NewKeyFrom(model.Follow.Prefix, userID.Bytes(), followedID.Bytes())
	if err := db.Set(followKey, []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := model.EntryIndex.Index(db, userID, time.Unix(100, 0), []byte("own-entry")); err != nil {
		t.Fatal(err)
	}
	if err := model.EntryIndex.Index(db, followedID, time.Unix(200, 0), []byte("followed-entry")); err != nil {
		t.Fatal(err)
	}
	timelineID := model.UniqueKeyFrom(fmt.Sprintf("%x", userID), "user", "timeline")
	if err := model.EntryIndex.Index(db, timelineID, time.Unix(300, 0), []byte("stale-entry")); err != nil {
		t.Fatal(err)
	}

	dryStats, err := rebuildTimelines(db, timelineRebuildOptions{user: "user", maxLimit: 1, dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dryStats.profiles != 1 || dryStats.existing != 1 || dryStats.follows != 0 || dryStats.entries != 1 {
		t.Fatalf("unexpected dry-run stats: %+v", dryStats)
	}
	stalePrefix := model.NewUUIDKey(model.TableEntryIndex, timelineID)
	staleCount, err := db.ForwardScan(stalePrefix, func(i int, key, value []byte) error { return nil })
	if err != nil || staleCount != 1 {
		t.Fatalf("dry-run modified timeline: count=%d, err=%v", staleCount, err)
	}

	stats, err := rebuildTimelines(db, timelineRebuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.profiles != 1 || stats.follows != 1 || stats.entries != 2 {
		t.Fatalf("unexpected rebuild stats: %+v", stats)
	}

	timelinePrefix := model.NewUUIDKey(model.TableEntryIndex, timelineID)
	var values []string
	if _, err := db.ForwardScan(timelinePrefix, func(i int, key, value []byte) error {
		values = append(values, string(value))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != "followed-entry" || values[1] != "own-entry" {
		t.Fatalf("unexpected rebuilt timeline: %v", values)
	}
}

func TestExplicitTimelineUserDoesNotRequireOAuthMetadata(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	userID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{Uuid: userID.String(), Id: "yinhm", Type: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := model.EntryIndex.Index(db, userID, time.Unix(100, 0), []byte("own-entry")); err != nil {
		t.Fatal(err)
	}

	stats, err := rebuildTimelines(db, timelineRebuildOptions{user: "yinhm", maxLimit: 20, dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.profiles != 1 || stats.entries != 1 {
		t.Fatalf("unexpected explicit-user stats: %+v", stats)
	}
}

func TestRebuildSocialGraphFromLegacyFeedinfo(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	followerID := uuid.Must(uuid.NewV4())
	feedID := uuid.Must(uuid.NewV4())
	follower := &pb.Profile{Uuid: followerID.String(), Id: "follower", Type: "user"}
	feed := &pb.Profile{Uuid: feedID.String(), Id: "feed", Type: "user"}
	for _, profile := range []*pb.Profile{follower, feed} {
		if err := model.UpdateProfile(db, profile); err != nil {
			t.Fatal(err)
		}
	}

	// The current field names intentionally hold legacy wire semantics:
	// Feedinfo.Followers (field 11) was subscriptions.
	if err := model.PutFeedinfo(db, follower.Uuid, &pb.Feedinfo{
		Uuid: follower.Uuid, Id: follower.Id, Followers: []*pb.Profile{feed},
	}); err != nil {
		t.Fatal(err)
	}
	// Feedinfo.Following (field 10) was subscribers. This is reciprocal
	// evidence for the same edge and must be deduplicated.
	if err := model.PutFeedinfo(db, feed.Uuid, &pb.Feedinfo{
		Uuid: feed.Uuid, Id: feed.Id, Following: []*pb.Profile{follower},
	}); err != nil {
		t.Fatal(err)
	}

	dryStats, err := rebuildSocialGraph(db, true)
	if err != nil {
		t.Fatal(err)
	}
	followExists, err := db.Exists(model.NewKeyFrom(model.Follow.Prefix, followerID.Bytes(), feedID.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if dryStats.edges != 1 || followExists {
		t.Fatalf("unexpected social graph dry run: %+v", dryStats)
	}

	stats, err := rebuildSocialGraph(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.feedinfos != 2 || stats.edges != 1 || stats.skipped != 0 {
		t.Fatalf("unexpected social graph stats: %+v", stats)
	}
	followKey := model.NewKeyFrom(model.Follow.Prefix, followerID.Bytes(), feedID.Bytes())
	followerKey := model.NewKeyFrom(model.Follower.Prefix, feedID.Bytes(), followerID.Bytes())
	followExists, err = db.Exists(followKey)
	if err != nil {
		t.Fatal(err)
	}
	followerExists, err := db.Exists(followerKey)
	if err != nil {
		t.Fatal(err)
	}
	if !followExists || !followerExists {
		t.Fatal("rebuild did not create both Follow and Follower keys")
	}
}
