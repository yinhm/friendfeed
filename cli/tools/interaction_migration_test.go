package main

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestMigrateInteractionsDryRunApplyAndReopen(t *testing.T) {
	dbPath := t.TempDir()
	db, err := store.NewStore(dbPath)
	require.NoError(t, err)

	entryUUID := uuid.Must(uuid.NewV4())
	authorUUID := seedActorProfile(t, db, "interaction-author")
	actorUUID := seedActorProfile(t, db, "interaction-actor")
	commentOne := uuid.Must(uuid.NewV4())
	commentTwo := uuid.Must(uuid.NewV4())
	legacy := &pb.Entry{
		Id: entryUUID.String(), ProfileUuid: authorUUID.String(),
		Likes: []*pb.Like{{Date: "2026-01-01T00:00:00Z", From: &pb.Feed{Uuid: actorUUID.String(), Id: "interaction-actor"}}},
		Comments: []*pb.Comment{
			{Id: commentTwo.String(), Date: "2026-01-02T00:00:00Z", From: &pb.Feed{Uuid: actorUUID.String()}},
			{Id: commentOne.String(), Date: "2026-01-01T00:00:00Z", From: &pb.Feed{Uuid: actorUUID.String()}},
		},
	}
	_, err = model.Entry.Put(db, entryUUID.Bytes(), legacy)
	require.NoError(t, err)

	dryStats, err := migrateInteractions(db, interactionMigrationOptions{dryRun: true})
	require.NoError(t, err)
	require.Equal(t, interactionMigrationStats{
		entriesScanned: 1, entriesMigrated: 1, likes: 1, comments: 2,
	}, dryStats)
	raw := new(pb.Entry)
	require.NoError(t, model.Entry.Get(db, entryUUID.Bytes(), raw))
	require.Len(t, raw.Likes, 1)
	require.Len(t, raw.Comments, 2)

	stats, err := migrateInteractions(db, interactionMigrationOptions{})
	require.NoError(t, err)
	require.Equal(t, dryStats, stats)
	require.NoError(t, db.CloseWithError())

	db, err = store.NewStore(dbPath)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, model.Entry.Get(db, entryUUID.Bytes(), raw))
	require.Empty(t, raw.Likes)
	require.Empty(t, raw.Comments)
	hydrated, err := model.GetEntry(db, entryUUID.String())
	require.NoError(t, err)
	require.Len(t, hydrated.Likes, 1)
	require.Len(t, hydrated.Comments, 2)
	require.Equal(t, commentOne.String(), hydrated.Comments[0].Id)
	require.Equal(t, commentTwo.String(), hydrated.Comments[1].Id)
}

func TestMigrateInteractionsPreservesUnresolvedArchiveInteractions(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	entryUUID := uuid.Must(uuid.NewV4())
	legacy := &pb.Entry{
		Id: entryUUID.String(),
		Likes: []*pb.Like{
			{Date: "2009-01-01T00:00:00Z", From: &pb.Feed{Id: "archived-user", Name: "Archived User"}},
			{Date: "2009-01-02T00:00:00Z", From: nil},
		},
		Comments: []*pb.Comment{
			{Id: "legacy-comment-id", Date: "2009-01-03T00:00:00Z", Body: "archive", From: &pb.Feed{Id: "archived-user", Uuid: "invalid"}},
		},
	}
	_, err = model.Entry.Put(db, entryUUID.Bytes(), legacy)
	require.NoError(t, err)

	stats, err := migrateInteractions(db, interactionMigrationOptions{})
	require.NoError(t, err)
	require.Equal(t, interactionMigrationStats{
		entriesScanned: 1, entriesMigrated: 1, likes: 2, comments: 1,
		legacyActors: 3, generatedIDs: 1,
	}, stats)

	hydrated, err := model.GetEntry(db, entryUUID.String())
	require.NoError(t, err)
	require.Len(t, hydrated.Likes, 2)
	require.Len(t, hydrated.Comments, 1)
	var namedLike *pb.Like
	var anonymousLike *pb.Like
	for _, like := range hydrated.Likes {
		if like.From == nil || like.From.Name == "Unknown" {
			anonymousLike = like
		} else if like.From.Id == "archived-user" {
			namedLike = like
		}
	}
	require.NotNil(t, namedLike)
	require.Equal(t, "Archived User", namedLike.From.Name)
	require.Empty(t, namedLike.From.Uuid, "unresolved snapshots must grant no ownership")
	require.NotNil(t, anonymousLike)
	require.NotNil(t, anonymousLike.From, "migration must give web clients a renderable empty actor snapshot")
	require.Equal(t, "Unknown", anonymousLike.From.Name)
	expectedCommentID := legacyInteractionRowUUID(entryUUID, "comment", 0)
	require.Equal(t, expectedCommentID.String(), hydrated.Comments[0].Id)
	require.Empty(t, hydrated.Comments[0].From.Uuid)

	_, err = db.Get(model.LikeKey(entryUUID, legacyInteractionRowUUID(entryUUID, "like", 0)))
	require.NoError(t, err)
	_, err = db.Get(model.LikeKey(entryUUID, legacyInteractionRowUUID(entryUUID, "like", 1)))
	require.NoError(t, err)
}

func TestMigrateInteractionsValidatesAllEntriesBeforeWriting(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	actorUUID := seedActorProfile(t, db, "interaction-validation-actor")
	validEntryUUID := uuid.Must(uuid.NewV4())
	duplicateEntryUUID := uuid.Must(uuid.NewV4())
	_, err = model.Entry.Put(db, validEntryUUID.Bytes(), &pb.Entry{
		Id: validEntryUUID.String(), Likes: []*pb.Like{{From: &pb.Feed{Uuid: actorUUID.String()}}},
	})
	require.NoError(t, err)
	_, err = model.Entry.Put(db, duplicateEntryUUID.Bytes(), &pb.Entry{
		Id: duplicateEntryUUID.String(), Likes: []*pb.Like{
			{From: &pb.Feed{Uuid: actorUUID.String()}},
			{From: &pb.Feed{Uuid: actorUUID.String()}},
		},
	})
	require.NoError(t, err)

	stats, err := migrateInteractions(db, interactionMigrationOptions{})
	require.ErrorContains(t, err, "interaction migration validation failed")
	require.Equal(t, 2, stats.entriesScanned)
	require.Equal(t, 1, stats.duplicates)

	raw := new(pb.Entry)
	require.NoError(t, model.Entry.Get(db, validEntryUUID.Bytes(), raw))
	require.Len(t, raw.Likes, 1)
	_, err = db.Get(model.LikeKey(validEntryUUID, actorUUID))
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestMigrateInteractionsForOneUserPreservesIdentityAndPermissions(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	authorUUID := seedActorProfile(t, db, "migration-author")
	actorUUID := seedActorProfile(t, db, "renamed-actor")
	otherAuthorUUID := seedActorProfile(t, db, "other-author")
	commentOne := uuid.Must(uuid.NewV4())
	commentTwo := uuid.Must(uuid.NewV4())
	entryUUID := seedActorEntry(t, db, &pb.Entry{
		ProfileUuid: authorUUID.String(),
		Likes: []*pb.Like{{
			Date: "2026-01-01T00:00:00Z",
			From: &pb.Feed{Uuid: actorUUID.String(), Id: "actor-before-rename"},
		}},
		Comments: []*pb.Comment{
			{Id: commentTwo.String(), Date: "2026-01-02T00:00:00Z", Body: "second", From: &pb.Feed{Uuid: actorUUID.String(), Id: "actor-before-rename"}},
			{Id: commentOne.String(), Date: "2026-01-01T00:00:00Z", Body: "first", From: &pb.Feed{Uuid: actorUUID.String(), Id: "actor-before-rename"}},
		},
	})
	otherEntryUUID := seedActorEntry(t, db, &pb.Entry{
		ProfileUuid: otherAuthorUUID.String(),
		Likes:       []*pb.Like{{From: &pb.Feed{Uuid: actorUUID.String()}}},
	})

	stats, err := migrateInteractions(db, interactionMigrationOptions{user: "migration-author", maxLimit: 1})
	require.NoError(t, err)
	require.Equal(t, interactionMigrationStats{
		entriesScanned: 1, entriesMigrated: 1, likes: 1, comments: 2,
	}, stats)

	entry, err := model.GetEntry(db, entryUUID.String())
	require.NoError(t, err)
	require.Len(t, entry.Likes, 1)
	require.Len(t, entry.Comments, 2)
	require.Equal(t, commentOne.String(), entry.Comments[0].Id)
	require.Equal(t, commentTwo.String(), entry.Comments[1].Id)

	actor, err := model.GetProfileFromUuid(db, actorUUID)
	require.NoError(t, err)
	_, entry, err = model.PutLike(db, actor, entry)
	require.NoError(t, err)
	require.Len(t, entry.Likes, 1, "stable UUID must deduplicate a like after rename")
	_, entry, err = model.PutComment(db, actor, entry, &pb.Comment{Id: commentOne.String(), Body: "edited"})
	require.NoError(t, err)
	require.Equal(t, "edited", entry.Comments[0].Body)
	entry, err = model.DeleteLike(db, actor, entry)
	require.NoError(t, err)
	require.Empty(t, entry.Likes)

	otherRaw := new(pb.Entry)
	require.NoError(t, model.Entry.Get(db, otherEntryUUID.Bytes(), otherRaw))
	require.Len(t, otherRaw.Likes, 1, "user-scoped migration must not touch other authors")
}
