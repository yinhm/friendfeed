package main

import (
	"bytes"
	"errors"
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

func TestPurgeTimelineTables(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	viewer := uuid.Must(uuid.NewV4())
	entry := uuid.Must(uuid.NewV4())
	_, err = model.MoveTimelineEntry(db, viewer, entry, time.Now().UTC(), nil)
	require.NoError(t, err)
	require.NoError(t, model.TouchTimelineState(db, viewer, time.Now().UTC()))

	removed, err := purgeTimelineTables(db)
	require.NoError(t, err)
	require.Equal(t, map[string]int{
		"TimelineIndex": 1, "TimelinePosition": 1, "TimelineState": 1,
	}, removed)
	for _, prefix := range []store.Key{
		model.TimelineIndex.Prefix, model.TimelinePosition.Prefix, model.TimelineState.Prefix,
	} {
		remaining, err := db.ForwardScan(prefix, func(int, []byte, []byte) error { return nil })
		require.NoError(t, err)
		require.Zero(t, remaining)
	}
	if !destructiveCommands["purge_timeline"] {
		t.Fatal("purge_timeline is not marked destructive")
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
			want: "https://m.friendfeed.me/b4c16ec30ea16e9cf98d138cd274a8676f1e3b96",
			ok:   true,
		},
		{
			name: "friendfeed image CDN",
			in:   "http://i.friendfeed.com/5516ac2b193edd120573963d53ddcf79fbd46188",
			want: "https://m.friendfeed.me/5516ac2b193edd120573963d53ddcf79fbd46188",
			ok:   true,
		},
		{
			name: "friendfeed image CDN https",
			in:   "https://i.friendfeed.com/5516ac2b193edd120573963d53ddcf79fbd46188",
			want: "https://m.friendfeed.me/5516ac2b193edd120573963d53ddcf79fbd46188",
			ok:   true,
		},
		{
			name: "bare friendfeed-media host",
			in:   "http://friendfeed-media.com/b4c16ec30ea16e9cf98d138cd274a8676f1e3b96",
			want: "https://m.friendfeed.me/b4c16ec30ea16e9cf98d138cd274a8676f1e3b96",
			ok:   true,
		},
		{
			name: "GCS http",
			in:   "http://storage.googleapis.com/lastff01/p-avatar-large",
			want: "https://m.friendfeed.me/p-avatar-large",
			ok:   true,
		},
		{
			name: "GCS other bucket untouched",
			in:   "https://storage.googleapis.com/otherbucket/p-avatar-large",
			want: "https://storage.googleapis.com/otherbucket/p-avatar-large",
		},
		{
			name: "scheme self-repair",
			in:   "http://m.friendfeed.me/image.jpg",
			want: "https://m.friendfeed.me/image.jpg",
			ok:   true,
		},
		{name: "external image", in: "https://example.com/image.jpg", want: "https://example.com/image.jpg"},
		{name: "twitter CDN untouched", in: "http://pbs.twimg.com/media/ABC.jpg", want: "http://pbs.twimg.com/media/ABC.jpg"},
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

func TestMigrateMediaText(t *testing.T) {
	// Image URLs on retired hosts are rewritten; external links are not.
	body := `<div class="media"><a href="http://online.wsj.com/article/SB124535285705228571.html" aria-label="Open media"><img alt="" src="http://i.friendfeed.com/5516ac2b193edd120573963d53ddcf79fbd46188" style="width: 262px; height: 174px;"></a></div>`
	got, ok := migrateMediaText(body)
	if !ok {
		t.Fatal("expected body to be rewritten")
	}
	if !strings.Contains(got, `src="https://m.friendfeed.me/5516ac2b193edd120573963d53ddcf79fbd46188"`) {
		t.Fatalf("image src not migrated: %q", got)
	}
	if !strings.Contains(got, `href="http://online.wsj.com/article/SB124535285705228571.html"`) {
		t.Fatalf("external link was changed: %q", got)
	}

	// Media links on the retired friendfeed-media host are rewritten too, and
	// already-migrated http URLs are upgraded to https.
	linked := `<div class="media"><a href="http://m.friendfeed-media.com/07a1ee699cef1999e03bcbaaec661ef77ac8852d" aria-label="Open media"><img alt="" src="http://m.friendfeed.me/46b97c2da4b7596dfb4f78613d65080cbdca2439" style="width: 405px; height: 175px;"></a></div>`
	got, ok = migrateMediaText(linked)
	if !ok {
		t.Fatal("expected linked media body to be rewritten")
	}
	if !strings.Contains(got, `href="https://m.friendfeed.me/07a1ee699cef1999e03bcbaaec661ef77ac8852d"`) {
		t.Fatalf("media link not migrated: %q", got)
	}
	if !strings.Contains(got, `src="https://m.friendfeed.me/46b97c2da4b7596dfb4f78613d65080cbdca2439"`) {
		t.Fatalf("image src not upgraded to https: %q", got)
	}

	if _, ok := migrateMediaText(`<p>plain text</p>`); ok {
		t.Fatal("plain text should not be rewritten")
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
			{Url: "http://m.friendfeed-media.com/thumbnail",
				Link: "http://m.friendfeed-media.com/07a1ee699cef1999e03bcbaaec661ef77ac8852d"},
			{Url: "https://example.com/external.jpg"},
		},
		Body: `<div class="media"><a href="http://online.wsj.com/article/SB124535285705228571.html" aria-label="Open media"><img alt="" src="http://i.friendfeed.com/5516ac2b193edd120573963d53ddcf79fbd46188" style="width: 262px; height: 174px;"></a></div>`,
	}
	if _, err := model.Entry.Put(db, entryID.Bytes(), entry); err != nil {
		t.Fatal(err)
	}

	dryStats, err := migrateMediaURLs(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if dryStats.profiles != 1 || dryStats.entries != 1 || dryStats.thumbnails != 1 ||
		dryStats.links != 1 || dryStats.bodies != 1 {
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
	if got := migratedEntry.Thumbnails[0].Url; got != "https://m.friendfeed.me/thumbnail" {
		t.Fatalf("unexpected thumbnail URL: %q", got)
	}
	if got := migratedEntry.Thumbnails[0].Link; got != "https://m.friendfeed.me/07a1ee699cef1999e03bcbaaec661ef77ac8852d" {
		t.Fatalf("unexpected thumbnail link: %q", got)
	}
	if got := migratedEntry.Thumbnails[1].Url; got != "https://example.com/external.jpg" {
		t.Fatalf("external thumbnail was changed: %q", got)
	}
	if !strings.Contains(migratedEntry.Body, `src="https://m.friendfeed.me/5516ac2b193edd120573963d53ddcf79fbd46188"`) {
		t.Fatalf("body image not migrated: %q", migratedEntry.Body)
	}
	if !strings.Contains(migratedEntry.Body, "http://online.wsj.com/") {
		t.Fatalf("body external link was changed: %q", migratedEntry.Body)
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
	ownEntry := uuid.Must(uuid.NewV4())
	followedEntry := uuid.Must(uuid.NewV4())
	for id, author := range map[uuid.UUID]uuid.UUID{ownEntry: userID, followedEntry: followedID} {
		if _, err := model.PutEntry(db, &pb.Entry{
			Id: id.String(), ProfileUuid: author.String(),
			Date: time.Unix(100, 0).UTC().Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
	}

	dryStats, err := rebuildTimelines(db, timelineRebuildOptions{user: "user", maxLimit: 1, dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dryStats.profiles != 1 || dryStats.existing != 0 || dryStats.follows != 0 || dryStats.entries != 1 {
		t.Fatalf("unexpected dry-run stats: %+v", dryStats)
	}
	stalePrefix := model.TimelineIndexPrefix(userID)
	staleCount, err := db.ForwardScan(stalePrefix, func(i int, key, value []byte) error { return nil })
	if err != nil || staleCount != 0 {
		t.Fatalf("dry-run modified timeline: count=%d, err=%v", staleCount, err)
	}
	stats, err := rebuildTimelines(db, timelineRebuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.profiles != 1 || stats.follows != 1 || stats.entries != 2 {
		t.Fatalf("unexpected rebuild stats: %+v", stats)
	}

	timelinePrefix := model.TimelineIndexPrefix(userID)
	var values []uuid.UUID
	if _, err := db.ForwardScan(timelinePrefix, func(i int, key, value []byte) error {
		_, entryID, _, err := model.ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		values = append(values, entryID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("unexpected rebuilt timeline: %v", values)
	}
	active, err := model.TimelineIsActive(db, userID, time.Now().UTC())
	if err != nil || !active {
		t.Fatalf("OAuth profile was not prewarmed: active=%v, err=%v", active, err)
	}
}

func TestRebuildTimelinesSkipsProfileWithoutOAuth(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	oauthID := uuid.Must(uuid.NewV4())
	nonOAuthID := uuid.Must(uuid.NewV4())
	for id, name := range map[uuid.UUID]string{oauthID: "oauth-user", nonOAuthID: "profile-only"} {
		require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: id.String(), Id: name, Type: "user"}))
		_, err := model.PutEntry(db, &pb.Entry{
			Id: uuid.Must(uuid.NewV4()).String(), ProfileUuid: id.String(),
			Date: time.Unix(100, 0).UTC().Format(time.RFC3339),
		})
		require.NoError(t, err)
	}
	_, err = model.OAuth.Put(db, model.KeyFromString("google", "oauth-user"), &pb.OAuthUser{
		Uuid: oauthID.String(), Provider: "google", UserId: "oauth-user",
	})
	require.NoError(t, err)

	stats, err := rebuildTimelines(db, timelineRebuildOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, stats.profiles)
	active, err := model.TimelineIsActive(db, oauthID, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, active)
	active, err = model.TimelineIsActive(db, nonOAuthID, time.Now().UTC())
	require.NoError(t, err)
	require.False(t, active)
}

func TestExplicitTimelineUserDoesNotRequireOAuthMetadata(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	userID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{Uuid: userID.String(), Id: "yinhm", Type: "user"}); err != nil {
		t.Fatal(err)
	}
	if _, err := model.PutEntry(db, &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), ProfileUuid: userID.String(),
		Date: time.Unix(100, 0).UTC().Format(time.RFC3339),
	}); err != nil {
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

func TestCollectTimelineCandidatesMergesFeedsAndBoundsRows(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	firstFeed := uuid.Must(uuid.NewV4())
	secondFeed := uuid.Must(uuid.NewV4())
	base := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = uuid.Must(uuid.NewV4())
		feed := firstFeed
		if i%2 == 1 {
			feed = secondFeed
		}
		published := base.Add(-time.Duration(i) * time.Hour)
		_, err := model.Entry.Put(db, ids[i].Bytes(), &pb.Entry{Id: ids[i].String(), ProfileUuid: feed.String(), Date: published.Format(time.RFC3339)})
		require.NoError(t, err)
		require.NoError(t, model.EntryIndex.Index(db, feed, published, model.Entry.PrefixAppend(ids[i].Bytes())))
	}
	// One entry appearing in both feeds must consume only one candidate slot.
	require.NoError(t, model.EntryIndex.Index(db, secondFeed, base, model.Entry.PrefixAppend(ids[0].Bytes())))

	rows, _, err := model.BuildHomeTimeline(db, []uuid.UUID{firstFeed, secondFeed}, 3, model.TimelineRetentionMax, base)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	for _, id := range ids[:3] {
		require.Contains(t, rows, id)
	}
	for _, id := range ids[3:] {
		require.NotContains(t, rows, id)
	}
}

func TestCollectTimelineCandidatesAppliesOptionalRetention(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed := uuid.Must(uuid.NewV4())
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	recent := uuid.Must(uuid.NewV4())
	old := uuid.Must(uuid.NewV4())
	_, err = model.Entry.Put(db, recent.Bytes(), &pb.Entry{Id: recent.String(), ProfileUuid: feed.String(), Date: now.Add(-89 * 24 * time.Hour).Format(time.RFC3339)})
	require.NoError(t, err)
	_, err = model.Entry.Put(db, old.Bytes(), &pb.Entry{Id: old.String(), ProfileUuid: feed.String(), Date: now.Add(-91 * 24 * time.Hour).Format(time.RFC3339)})
	require.NoError(t, err)
	require.NoError(t, model.EntryIndex.Index(db, feed, now.Add(-89*24*time.Hour), model.Entry.PrefixAppend(recent.Bytes())))
	require.NoError(t, model.EntryIndex.Index(db, feed, now.Add(-91*24*time.Hour), model.Entry.PrefixAppend(old.Bytes())))

	limited, _, err := model.BuildHomeTimeline(db, []uuid.UUID{feed}, 10, 90*24*time.Hour, now)
	require.NoError(t, err)
	require.Contains(t, limited, recent)
	require.NotContains(t, limited, old)

	unlimited, _, err := model.BuildHomeTimeline(db, []uuid.UUID{feed}, 10, model.TimelineRetentionMax, now)
	require.NoError(t, err)
	require.Contains(t, unlimited, recent)
	require.Contains(t, unlimited, old)
}

func TestRebuildTimelineRejectsLegacyEntryIndexKey(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	profileID := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: profileID.String(), Id: "legacy-index", Type: "user"}
	require.NoError(t, model.UpdateProfile(db, profile))
	entry := &pb.Entry{Id: entryID.String(), ProfileUuid: profileID.String(), Date: time.Now().UTC().Format(time.RFC3339)}
	_, err = model.Entry.Put(db, entryID.Bytes(), entry)
	require.NoError(t, err)
	legacyIndexKey := store.NewUUIDFlakeKey(model.TableEntryIndex, profileID, db.TimeTravelReverseId(time.Now())).Bytes()
	require.NoError(t, db.Put(legacyIndexKey, model.Entry.PrefixAppend(entryID.Bytes())))

	stats, err := rebuildTimelines(db, timelineRebuildOptions{user: profile.Id})
	require.ErrorContains(t, err, "invalid EntryIndex key length")
	require.Zero(t, stats.entries)
	count, err := db.ForwardScan(model.TimelineIndexPrefix(profileID), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Zero(t, count)
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

func TestRebuildPublicTimelineFiltersPrivateFeeds(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	db.SetSync(false)

	publicID := uuid.Must(uuid.NewV4())
	privateID := uuid.Must(uuid.NewV4())
	deletedID := uuid.Must(uuid.NewV4())
	for _, profile := range map[uuid.UUID]*pb.Profile{
		publicID:  {Uuid: publicID.String(), Id: "public-user", Type: "user"},
		privateID: {Uuid: privateID.String(), Id: "private-user", Type: "user", Private: true},
		deletedID: {Uuid: deletedID.String(), Id: "deleted-user", Type: "user", Deleted: true},
	} {
		if err := model.UpdateProfile(db, profile); err != nil {
			t.Fatal(err)
		}
	}

	published := time.Unix(100, 0).UTC()
	publicEntry := uuid.Must(uuid.NewV4())
	privateEntry := uuid.Must(uuid.NewV4())
	deletedEntry := uuid.Must(uuid.NewV4())
	for entryID, author := range map[uuid.UUID]uuid.UUID{
		publicEntry: publicID, privateEntry: privateID, deletedEntry: deletedID,
	} {
		if _, err := model.PutEntry(db, &pb.Entry{
			Id: entryID.String(), ProfileUuid: author.String(),
			Date: published.Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Dry-run reports but does not write.
	if err := rebuildPublicTimeline(db, timelineRebuildOptions{dryRun: true}); err != nil {
		t.Fatal(err)
	}
	dryRows, err := db.ForwardScan(model.TimelineIndexPrefix(model.PublicTimelineUUID), func(int, []byte, []byte) error { return nil })
	if err != nil || dryRows != 0 {
		t.Fatalf("dry-run modified public timeline: rows=%d, err=%v", dryRows, err)
	}

	if err := rebuildPublicTimeline(db, timelineRebuildOptions{}); err != nil {
		t.Fatal(err)
	}
	position, err := model.TimelinePositionTime(db, model.PublicTimelineUUID, publicEntry)
	if err != nil || !position.Equal(published) {
		t.Fatalf("public entry activity = %v, want %v (err=%v)", position, published, err)
	}
	for _, excluded := range []uuid.UUID{privateEntry, deletedEntry} {
		if _, err := model.TimelinePositionTime(db, model.PublicTimelineUUID, excluded); err == nil {
			t.Fatalf("entry %s must stay out of the public timeline", excluded)
		}
	}
	// The public viewer never gets a Home TimelineState row.
	if _, err := model.TimelineLastAccess(db, model.PublicTimelineUUID); err == nil {
		t.Fatal("public timeline must not carry TimelineState")
	}
}

func TestPurgePublicCache(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	db.SetSync(false)

	key := model.NewUUIDKey(model.TableMeta, uuid.NewV5(uuid.NamespaceURL, "index:public:cache"))
	if err := db.Put(key, []byte("retired gob blob")); err != nil {
		t.Fatal(err)
	}

	dryRun = true
	runPurgePublicCacheCommand(db)
	if _, err := db.Get(key); err != nil {
		t.Fatalf("dry-run must keep the cache row: %v", err)
	}

	dryRun = false
	runPurgePublicCacheCommand(db)
	if _, err := db.Get(key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cache row should be deleted, err=%v", err)
	}

	// Idempotent when the row is already absent.
	runPurgePublicCacheCommand(db)
}
