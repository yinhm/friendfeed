package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestRebuildHomeTimelineActivityReplaysCurrentInteractions(t *testing.T) {
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	entry := &pb.Entry{
		Date: base.Format(time.RFC3339),
		Likes: []*pb.Like{
			{Date: base.Add(5 * time.Minute).Format(time.RFC3339), From: &pb.Feed{Uuid: "a"}},
			{Date: base.Add(15 * time.Minute).Format(time.RFC3339), From: &pb.Feed{Uuid: "b"}},
		},
		Comments: []*pb.Comment{
			{Id: "c", Date: base.Add(16 * time.Minute).Format(time.RFC3339)},
		},
	}

	activity, skipped, err := rebuildHomeTimelineActivity(entry, base.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, base.Add(16*time.Minute), activity)
}

func TestRebuildHomeTimelineActivitySkipsUndatedLegacyInteractions(t *testing.T) {
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	entry := &pb.Entry{
		Date:     base.Format(time.RFC3339),
		Likes:    []*pb.Like{{Date: ""}, {Date: "invalid"}, nil},
		Comments: []*pb.Comment{{Date: base.Add(time.Hour).Format(time.RFC3339)}, {Date: ""}, nil},
	}

	activity, skipped, err := rebuildHomeTimelineActivity(entry, base.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 5, skipped)
	require.Equal(t, base.Add(time.Hour), activity)

	entry.Date = ""
	_, _, err = rebuildHomeTimelineActivity(entry, base.Add(2*time.Hour))
	require.ErrorContains(t, err, "invalid publish date")
}

func TestLoadHomeTimelineActivitiesUsesIndependentInteractions(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	author := uuid.Must(uuid.NewV4())
	actor := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	commentID := uuid.Must(uuid.NewV4())
	_, err = PutEntry(db, &pb.Entry{
		Id: entryID.String(), ProfileUuid: author.String(), Date: base.Format(time.RFC3339),
		Likes: []*pb.Like{{
			Date: base.Add(15 * time.Minute).Format(time.RFC3339),
			From: &pb.Feed{Uuid: actor.String()},
		}},
		Comments: []*pb.Comment{{
			Id: commentID.String(), Date: base.Add(16 * time.Minute).Format(time.RFC3339),
			From: &pb.Feed{Uuid: actor.String()},
		}},
	})
	require.NoError(t, err)

	missing := uuid.Must(uuid.NewV4())
	rows := map[uuid.UUID]time.Time{entryID: {}, missing: {}}
	skipped, err := loadHomeTimelineActivities(db, rows, base.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, base.Add(16*time.Minute), rows[entryID])
	_, exists := rows[missing]
	require.False(t, exists)
}
