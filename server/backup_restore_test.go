package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// TestBackupRestoreRoundTrip proves the production backup path
// (ApiServer.BackupDBTo, shared by BackupDB) produces a database that
// reopens standalone in a separate directory and serves the seeded data:
// public feedinfo metadata, profile + UserMap, UserRenameMap redirects,
// OAuth records, and entries with their author/group direct indexes.
func TestBackupRestoreRoundTrip(t *testing.T) {
	// model.PutEntry indexes entry bodies through the global search.Indexer;
	// install a mock and restore the previous index to avoid cross-test bleed.
	prevIndexer := search.Indexer
	search.Indexer = search.NewMockIndex()
	t.Cleanup(func() { search.Indexer = prevIndexer })

	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	require.NoError(t, err)

	sourcePath := filepath.Join(t.TempDir(), "source")
	backupPath := filepath.Join(t.TempDir(), "backup")

	srv := NewApiServer(sourcePath, cfg)
	defer srv.Shutdown()

	// --- seed the source database ---

	// Public feed metadata, written through the Feedinfo table path.
	publicUUID := uuid.Must(uuid.NewV4())
	publicFeedinfo := &pb.Feedinfo{
		Uuid:        publicUUID.String(),
		Id:          "public",
		Name:        "Everyone's feed",
		Type:        "group",
		Description: "public feed metadata",
	}
	require.NoError(t, model.PutFeedinfo(srv.rdb, publicUUID.String(), publicFeedinfo))

	// A user profile (UpdateProfile also creates the UserMap id->uuid entry).
	profileUUID := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.mdb, &pb.Profile{
		Uuid:        profileUUID.String(),
		Id:          "backupuser",
		Name:        "Backup User",
		Type:        "user",
		Description: "restore me",
	}))

	// An OAuth record for the same user.
	seededOAuth, err := model.PutOAuthUser(srv.mdb, &pb.OAuthUser{
		Provider:          "Twitter",
		UserId:            "233666",
		Name:              "backupuser",
		NickName:          "Backup User",
		AccessToken:       "token",
		AccessTokenSecret: "secret",
	})
	require.NoError(t, err)

	// A rename: moves the UserMap entry and records old id -> uuid in
	// UserRenameMap (table 7).
	require.NoError(t, model.RenameProfileId(srv.mdb, profileUUID, "renameduser"))

	// A group feed to hold the second entry's group direct index.
	groupUUID := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.mdb, &pb.Profile{
		Uuid: groupUUID.String(), Id: "backupgroup", Name: "Backup Group", Type: "group",
	}))

	// Entries through model.PutEntry so the entry record and the author/group
	// direct indexes land in one atomic commit. Dates differ because the
	// direct index key carries a reverse timestamp and same-timestamp keys
	// dedup.
	authorEntry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: profileUUID.String(),
		Date:        "2012-09-07T07:40:22Z",
		Body:        "author feed entry",
		From:        &pb.Feed{Uuid: profileUUID.String(), Id: "backupuser", Name: "Backup User"},
	}
	authorEntryKey, err := model.PutEntry(srv.rdb, authorEntry)
	require.NoError(t, err)

	groupEntry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: profileUUID.String(),
		FeedUuid:    groupUUID.String(),
		Date:        "2012-09-08T07:40:22Z",
		Body:        "group feed entry",
		From:        &pb.Feed{Uuid: profileUUID.String(), Id: "backupuser", Name: "Backup User"},
	}
	groupEntryKey, err := model.PutEntry(srv.rdb, groupEntry)
	require.NoError(t, err)

	// --- run the production backup logic into a standalone directory ---
	require.NoError(t, srv.BackupDBTo(backupPath))

	// --- verify the restored database key by key, read-only ---
	rodb := store.NewStoreReadOnly(backupPath)

	// The backup is a full copy: the key spaces match exactly.
	require.Equal(t, countKeys(t, srv.rdb), countKeys(t, rodb))

	// Public feedinfo metadata survives verbatim.
	gotFeedinfo, err := model.GetFeedinfo(rodb, publicUUID.String())
	require.NoError(t, err)
	require.True(t, proto.Equal(publicFeedinfo, gotFeedinfo),
		"public feedinfo mismatch: got %v", gotFeedinfo)

	// Profile readable by stable uuid, reflecting the rename.
	gotProfile, err := model.GetProfileFromUuid(rodb, profileUUID)
	require.NoError(t, err)
	require.Equal(t, "renameduser", gotProfile.Id)
	require.Equal(t, "Backup User", gotProfile.Name)

	// Profile resolvable by current id through UserMap; the raw value is
	// the 16-byte user UUID.
	byID, err := model.GetProfileFromUserId(rodb, "renameduser")
	require.NoError(t, err)
	require.Equal(t, profileUUID.String(), byID.Uuid)
	rawMap, err := model.UserMap.GetRaw(rodb, []byte("renameduser"))
	require.NoError(t, err)
	require.Len(t, rawMap, uuid.Size, "UserMap value must be a 16-byte UUID")
	require.Equal(t, profileUUID.Bytes(), rawMap)

	// The old id is gone from UserMap but resolves through UserRenameMap
	// (old_id -> 16-byte user UUID) to the same profile.
	_, err = model.GetProfileFromUserId(rodb, "backupuser")
	require.Error(t, err, "old id must not resolve through UserMap after rename")
	rawRename, err := model.UserRenameMap.GetRaw(rodb, []byte("backupuser"))
	require.NoError(t, err)
	require.Len(t, rawRename, uuid.Size, "UserRenameMap value must be a 16-byte UUID")
	require.Equal(t, profileUUID.Bytes(), rawRename)
	resolvedUUID, err := model.FindProfileRenameByOldId(rodb, "backupuser")
	require.NoError(t, err)
	require.Equal(t, profileUUID, resolvedUUID)
	byRename, err := model.GetProfileFromRenameId(rodb, "backupuser")
	require.NoError(t, err)
	require.Equal(t, profileUUID.String(), byRename.Uuid)

	// OAuth record survives verbatim.
	_, gotOAuth, err := model.GetOAuthUser(rodb, "Twitter", "233666")
	require.NoError(t, err)
	require.True(t, proto.Equal(seededOAuth, gotOAuth),
		"oauth record mismatch: got %v", gotOAuth)

	// Entry bodies readable, and the author/group direct index keys exist
	// and point at the entry records.
	gotAuthorEntry, err := model.GetEntry(rodb, authorEntry.Id)
	require.NoError(t, err)
	require.Equal(t, "author feed entry", gotAuthorEntry.Body)
	gotGroupEntry, err := model.GetEntry(rodb, groupEntry.Id)
	require.NoError(t, err)
	require.Equal(t, "group feed entry", gotGroupEntry.Body)

	require.Contains(t, entryIndexTargets(t, rodb, profileUUID), authorEntryKey.String(),
		"author direct index must point at the author entry")
	require.Contains(t, entryIndexTargets(t, rodb, profileUUID), groupEntryKey.String(),
		"author direct index must point at the group entry")
	require.Contains(t, entryIndexTargets(t, rodb, groupUUID), groupEntryKey.String(),
		"group direct index must point at the group entry")

	// Release the read-only handle before reopening the path read-write;
	// Pebble holds the database lock even for read-only opens.
	rodb.Close()

	// --- serve the restored database over real gRPC ---
	restored := NewApiServer(backupPath, cfg)
	defer restored.Shutdown()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	rpcServer := grpc.NewServer()
	pb.RegisterApiServer(rpcServer, restored)
	go rpcServer.Serve(ln)
	defer rpcServer.GracefulStop()

	conn, err := grpc.Dial(ln.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	client := pb.NewApiClient(conn)
	ctx := context.Background()

	// FetchFeed by the current id: UserMap resolution, direct index scan
	// and entry reads all come from the restored database.
	feed, err := client.FetchFeed(ctx, &pb.FeedRequest{Id: "renameduser", PageSize: 30})
	require.NoError(t, err)
	require.Equal(t, profileUUID.String(), feed.Uuid)
	require.Equal(t, "renameduser", feed.Id)
	require.Len(t, feed.Entries, 2)

	// FetchFeed by the previous id: the UserRenameMap redirect survived the
	// backup and resolves to the same feed.
	redirected, err := client.FetchFeed(ctx, &pb.FeedRequest{Id: "backupuser", PageSize: 30})
	require.NoError(t, err)
	require.Equal(t, profileUUID.String(), redirected.Uuid)
	require.Equal(t, "renameduser", redirected.Id)
	require.Len(t, redirected.Entries, 2)
}

// countKeys exhaustively iterates a store; the iterator is closed explicitly.
func countKeys(t *testing.T, db *store.Store) int {
	t.Helper()
	iter := db.Iterator()
	defer iter.Close()
	n := 0
	for iter.First(); iter.Valid(); iter.Next() {
		n++
	}
	require.NoError(t, iter.Error())
	return n
}

// TestStoreSnapshotIsolation proves the snapshot primitive BackupDBTo relies
// on: a snapshot observes the state as of its creation, so writes and deletes
// that land afterwards are invisible through it. This is what makes the
// backup point-in-time consistent under online writes.
func TestStoreSnapshotIsolation(t *testing.T) {
	db := store.NewStore(filepath.Join(t.TempDir(), "source"))
	defer db.Close()

	require.NoError(t, db.Put([]byte("k1"), []byte("v1")))

	snap := db.Snapshot()

	// Mutations after the snapshot must not leak into it.
	require.NoError(t, db.Put([]byte("k2"), []byte("v2")))
	require.NoError(t, db.Delete([]byte("k1")))

	iter := db.SnapshotIterator(snap)
	seen := map[string]string{}
	for iter.First(); iter.Valid(); iter.Next() {
		seen[string(iter.Key())] = string(iter.Value())
	}
	require.NoError(t, iter.Error())
	require.NoError(t, iter.Close())
	require.NoError(t, snap.Close())

	require.Equal(t, map[string]string{"k1": "v1"}, seen,
		"snapshot must see exactly the state at its creation")

	// The live store reflects the post-snapshot mutations.
	require.False(t, db.Exist([]byte("k1")))
	require.True(t, db.Exist([]byte("k2")))
}

// TestBackupDBToRequiresFreshDestination covers the stale-backup regression:
// rerunning a backup into an existing directory must fail instead of merging
// with the earlier copy (which would resurrect keys deleted since then), and
// a backup into a fresh directory reflects the source at backup time.
func TestBackupDBToRequiresFreshDestination(t *testing.T) {
	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	require.NoError(t, err)

	srv := NewApiServer(filepath.Join(t.TempDir(), "source"), cfg)
	defer srv.Shutdown()

	require.NoError(t, srv.rdb.Put([]byte("stay"), []byte("1")))
	require.NoError(t, srv.rdb.Put([]byte("deleted"), []byte("1")))

	dir1 := filepath.Join(t.TempDir(), "backup1")
	require.NoError(t, srv.BackupDBTo(dir1))

	// A second backup into the same directory must fail, not merge.
	require.Error(t, srv.BackupDBTo(dir1))

	// Mutate the source after the first backup: delete one key, add another.
	require.NoError(t, srv.rdb.Delete([]byte("deleted")))
	require.NoError(t, srv.rdb.Put([]byte("new"), []byte("1")))

	dir2 := filepath.Join(t.TempDir(), "backup2")
	require.NoError(t, srv.BackupDBTo(dir2))

	rodb := store.NewStoreReadOnly(dir2)
	defer rodb.Close()
	require.True(t, rodb.Exist([]byte("stay")))
	require.True(t, rodb.Exist([]byte("new")))
	require.False(t, rodb.Exist([]byte("deleted")),
		"key deleted before the second backup must not reappear in a fresh backup")
}

// TestBackupDBToConcurrentWrites is a smoke test: writing the source database
// while a backup runs must not break the backup, and the result opens and
// reads back fine (snapshot consistency itself is covered by
// TestStoreSnapshotIsolation).
func TestBackupDBToConcurrentWrites(t *testing.T) {
	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	require.NoError(t, err)

	srv := NewApiServer(filepath.Join(t.TempDir(), "source"), cfg)
	defer srv.Shutdown()

	const initial = 100
	for i := 0; i < initial; i++ {
		require.NoError(t, srv.rdb.Put([]byte(fmt.Sprintf("k%05d", i)), []byte("v")))
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := initial; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			key := []byte(fmt.Sprintf("k%05d", i))
			if err := srv.rdb.Put(key, []byte("v")); err != nil {
				return
			}
			_ = srv.rdb.Delete(key)
		}
	}()

	backupPath := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, srv.BackupDBTo(backupPath))
	close(stop)
	wg.Wait()

	rodb := store.NewStoreReadOnly(backupPath)
	defer rodb.Close()
	require.GreaterOrEqual(t, countKeys(t, rodb), initial,
		"backup taken after seeding must contain at least the seeded keys")
	got, err := rodb.Get([]byte("k00000"))
	require.NoError(t, err)
	require.Equal(t, []byte("v"), got)
}

// TestBackupDBToAtomicPublish covers the atomic-publish contract: the backup
// is assembled in a hidden sibling temp directory and only renamed to the
// final path once complete, so a successful run leaves no temp residue, an
// existing destination fails without side effects, and a failed backup
// removes its temp directory instead of publishing a half-written copy.
func TestBackupDBToAtomicPublish(t *testing.T) {
	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	require.NoError(t, err)

	srv := NewApiServer(filepath.Join(t.TempDir(), "source"), cfg)
	defer srv.Shutdown()
	require.NoError(t, srv.rdb.Put([]byte("k"), []byte("v")))

	parent := t.TempDir()
	dest := filepath.Join(parent, "backup")

	// A successful backup publishes dest and leaves no temp residue.
	require.NoError(t, srv.BackupDBTo(dest))
	require.DirExists(t, dest)
	leftovers, err := filepath.Glob(filepath.Join(parent, ".backup-tmp-*"))
	require.NoError(t, err)
	require.Empty(t, leftovers, "successful backup must not leave temp directories")

	// Residue from a crashed run is unrelated: the next backup ignores it and
	// it stays put for manual removal.
	stale := filepath.Join(parent, ".backup-tmp-stale")
	require.NoError(t, os.Mkdir(stale, 0755))
	require.NoError(t, srv.BackupDBTo(filepath.Join(parent, "backup2")))
	require.DirExists(t, stale, "backup must not touch unrelated temp residue")

	// An existing destination fails up front and creates no temp residue.
	require.Error(t, srv.BackupDBTo(dest))
	leftovers, err = filepath.Glob(filepath.Join(parent, ".backup-tmp-*"))
	require.NoError(t, err)
	require.Len(t, leftovers, 1, "only the manually created stale dir may remain")

	// A backup that fails at publish time (unpublishable empty destination)
	// returns an error and cleans up its temp directory. Run it from a test-owned
	// directory so an interrupted test cannot leave residue in the package tree.
	failureDir := t.TempDir()
	t.Chdir(failureDir)
	before, err := filepath.Glob(filepath.Join(failureDir, ".backup-tmp-*"))
	require.NoError(t, err)
	require.Error(t, srv.BackupDBTo(""))
	after, err := filepath.Glob(filepath.Join(failureDir, ".backup-tmp-*"))
	require.NoError(t, err)
	require.Equal(t, before, after, "failed backup must clean up its temp directory")
}

// entryIndexTargets lists the entry keys the direct index of indexUUID
// points at. ForwardScan closes its iterator internally.
func entryIndexTargets(t *testing.T, db *store.Store, indexUUID uuid.UUID) []string {
	t.Helper()
	prefix := store.NewUUIDKey(model.TableEntryIndex, indexUUID).Bytes()
	var targets []string
	_, err := db.ForwardScan(prefix, func(i int, k, v []byte) error {
		targets = append(targets, store.Key(v).String())
		return nil
	})
	require.NoError(t, err)
	return targets
}
