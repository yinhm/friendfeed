package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestConcurrentLikesDoNotLoseUpdates(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	public := NewFeedIndex(db, "public", uuid.Must(uuid.NewV4()))
	t.Cleanup(func() {
		public.Stop()
		db.Close()
	})
	srv := &ApiServer{
		mdb:    db,
		rdb:    db,
		cached: map[string]*FeedIndex{"public": public},
	}

	authorUUID := uuid.Must(uuid.NewV4())
	entryUUID := uuid.Must(uuid.NewV4())
	entry := &pb.Entry{
		Id:          entryUUID.String(),
		Date:        time.Now().UTC().Format(time.RFC3339),
		ProfileUuid: authorUUID.String(),
		From:        &pb.Feed{Uuid: authorUUID.String(), Id: "author"},
	}
	_, err = model.PutEntry(db, entry)
	require.NoError(t, err)

	users := []uuid.UUID{uuid.Must(uuid.NewV4()), uuid.Must(uuid.NewV4())}
	for i, userUUID := range users {
		require.NoError(t, model.UpdateProfile(db, &pb.Profile{
			Uuid: userUUID.String(), Id: "liker" + string(rune('a'+i)), Name: "Liker",
		}))
	}

	start := make(chan struct{})
	errs := make(chan error, len(users))
	var wg sync.WaitGroup
	for _, userUUID := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := srv.LikeEntry(context.Background(), &pb.LikeRequest{
				Entry: entryUUID.String(), User: userUUID.String(), Like: true,
			})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	stored, err := model.GetEntry(db, entryUUID.String())
	require.NoError(t, err)
	require.Len(t, stored.Likes, len(users))
}
