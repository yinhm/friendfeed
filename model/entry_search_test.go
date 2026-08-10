package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
)

func TestDeleteEntryRemovesSearchDocument(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	db.SetSync(false)

	idx, err := search.OpenIndex(t.TempDir())
	require.NoError(t, err)
	defer idx.Close()
	previous := search.Indexer
	search.Indexer = idx
	defer func() { search.Indexer = previous }()

	profileUUID := uuid.Must(uuid.NewV4())
	entryUUID := uuid.Must(uuid.NewV4())
	_, err = PutEntry(db, &pb.Entry{
		Id:          entryUUID.String(),
		Date:        time.Now().UTC().Format(time.RFC3339),
		Body:        "searchable body",
		ProfileUuid: profileUUID.String(),
		From:        &pb.Feed{Uuid: profileUUID.String(), Id: "user", Name: "User", Type: "user"},
	})
	require.NoError(t, err)

	count, err := idx.DocCount()
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)

	require.NoError(t, DeleteEntry(db, entryUUID.String()))

	count, err = idx.DocCount()
	require.NoError(t, err)
	require.Equal(t, uint64(0), count)
}
