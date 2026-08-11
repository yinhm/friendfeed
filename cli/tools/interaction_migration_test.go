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

func TestMigrateInteractionsValidatesAllEntriesBeforeWriting(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	actorUUID := seedActorProfile(t, db, "interaction-validation-actor")
	validEntryUUID := uuid.Must(uuid.NewV4())
	invalidEntryUUID := uuid.Must(uuid.NewV4())
	valid := &pb.Entry{
		Id:    validEntryUUID.String(),
		Likes: []*pb.Like{{From: &pb.Feed{Uuid: actorUUID.String()}}},
	}
	invalid := &pb.Entry{
		Id:    invalidEntryUUID.String(),
		Likes: []*pb.Like{{From: &pb.Feed{Uuid: "invalid"}}},
	}
	_, err = model.Entry.Put(db, validEntryUUID.Bytes(), valid)
	require.NoError(t, err)
	_, err = model.Entry.Put(db, invalidEntryUUID.Bytes(), invalid)
	require.NoError(t, err)

	stats, err := migrateInteractions(db, interactionMigrationOptions{})
	require.ErrorContains(t, err, "interaction migration validation failed")
	require.Equal(t, 2, stats.entriesScanned)
	require.Equal(t, 1, stats.invalidActors)

	raw := new(pb.Entry)
	require.NoError(t, model.Entry.Get(db, validEntryUUID.Bytes(), raw))
	require.Len(t, raw.Likes, 1)
	_, err = db.Get(model.LikeDataKey(validEntryUUID, actorUUID))
	require.ErrorIs(t, err, store.ErrNotFound)
}
