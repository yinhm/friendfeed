package task

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/store/flake"
)

func testTaskID() flake.Id {
	return flake.Id{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
}

func TestTaskIDRoundTrip(t *testing.T) {
	id := testTaskID()
	encoded := EncodeTaskID(id)
	require.Len(t, encoded, TaskIDHexSize)
	decoded, err := DecodeTaskID(encoded)
	require.NoError(t, err)
	require.Equal(t, id, decoded)
	_, err = DecodeTaskID("ABCDEF0123456789ABCDEF0123456789")
	require.ErrorContains(t, err, "lowercase")
}

func TestTaskQueueKeysRoundTrip(t *testing.T) {
	id := testTaskID()

	taskKey, err := TaskKey(id)
	require.NoError(t, err)
	parsedID, err := ParseTaskKey(taskKey)
	require.NoError(t, err)
	require.Equal(t, id, parsedID)

	ready, err := ReadyKey("rss.fetch", 1234, id)
	require.NoError(t, err)
	taskType, runAt, parsedID, err := ParseReadyKey(ready)
	require.NoError(t, err)
	require.Equal(t, "rss.fetch", taskType)
	require.EqualValues(t, 1234, runAt)
	require.Equal(t, id, parsedID)

	lease, err := LeaseKey(2345, id)
	require.NoError(t, err)
	leaseAt, parsedID, err := ParseLeaseKey(lease)
	require.NoError(t, err)
	require.EqualValues(t, 2345, leaseAt)
	require.Equal(t, id, parsedID)

	done, err := DoneKey(3456, id)
	require.NoError(t, err)
	doneAt, parsedID, err := ParseDoneKey(done)
	require.NoError(t, err)
	require.EqualValues(t, 3456, doneAt)
	require.Equal(t, id, parsedID)
}

func TestReadyKeysSortByTimeWithinType(t *testing.T) {
	id := testTaskID()
	first, err := ReadyKey("rss.fetch", 1, id)
	require.NoError(t, err)
	second, err := ReadyKey("rss.fetch", 2, id)
	require.NoError(t, err)
	require.Less(t, string(first), string(second))
}

func TestTaskQueueKeyValidation(t *testing.T) {
	id := testTaskID()
	for _, taskType := range []string{"", "RSS.fetch", "rss/fetch"} {
		_, err := ReadyKey(taskType, 1, id)
		require.Error(t, err)
	}
	_, err := ReadyKey("rss.fetch", -1, id)
	require.ErrorContains(t, err, "before Unix epoch")
	_, _, _, err = ParseReadyKey([]byte("broken"))
	require.Error(t, err)
	_, err = IdemKey("rss.fetch", "")
	require.Error(t, err)
	_, err = TaskKey(flake.Id{})
	require.ErrorContains(t, err, "must not be zero")
	_, err = ReadyKey("rss.fetch", 1, flake.Id{})
	require.ErrorContains(t, err, "must not be zero")
}

func TestIdemKeyIncludesTaskType(t *testing.T) {
	rss, err := IdemKey("rss.fetch", "same")
	require.NoError(t, err)
	media, err := IdemKey("media.fetch", "same")
	require.NoError(t, err)
	require.NotEqual(t, rss, media)
	digest, err := ParseIdemKey(rss)
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, digest)
}
