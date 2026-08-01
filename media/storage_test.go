package media

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/util"
)

// r2Recorder is a fake R2 endpoint recording PUT requests.
type r2Recorder struct {
	mu     sync.Mutex
	puts   []recordedPut
	status int
}

type recordedPut struct {
	path string
	body []byte
}

func (r *r2Recorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.puts = append(r.puts, recordedPut{path: req.URL.Path, body: body})
		r.mu.Unlock()
		w.WriteHeader(r.status)
	})
}

func (r *r2Recorder) recorded() []recordedPut {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedPut(nil), r.puts...)
}

// newDualWriteStorage builds a MirrorStorage backed by two httptest
// servers: the media origin and the fake R2 endpoint.
func newDualWriteStorage(t *testing.T, r2Status int) (*MirrorStorage, *httptest.Server, *r2Recorder) {
	t.Helper()
	content := []byte("dual write image bytes")
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(content)
	}))
	t.Cleanup(origin.Close)

	recorder := &r2Recorder{status: r2Status}
	r2srv := httptest.NewServer(recorder.handler())
	t.Cleanup(r2srv.Close)

	local := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	local.httpClient = origin.Client()
	ms := &MirrorStorage{
		local: local,
		r2: &R2Client{
			accessKeyID:     "ak",
			secretAccessKey: "sk",
			bucket:          "media",
			endpoint:        r2srv.URL,
			httpClient:      r2srv.Client(),
			now:             time.Now,
		},
	}
	return ms, origin, recorder
}

// Post writes to the local media directory and to R2 under the same
// object key; Url points at the public media base and Bucket carries the
// R2 bucket name.
func TestMirrorStorageDualWrite(t *testing.T) {
	ms, origin, recorder := newDualWriteStorage(t, http.StatusOK)

	obj, err := ms.FromUrl("", origin.URL+"/pic.jpg", "")
	assert.NoError(t, err)
	assert.Equal(t, "p/i/c.jpg", obj.Path)
	assert.Equal(t, "https://m.friendfeed.me/p/i/c.jpg", obj.Url)
	assert.Equal(t, "media", obj.Bucket)

	content := []byte("dual write image bytes")
	written, err := os.ReadFile(filepath.Join(ms.local.path, "p/i/c.jpg"))
	assert.NoError(t, err)
	assert.Equal(t, content, written)

	puts := recorder.recorded()
	assert.Len(t, puts, 1)
	assert.Equal(t, "/media/p/i/c.jpg", puts[0].path)
	assert.Equal(t, content, puts[0].body)
}

// An R2 failure fails the whole mirror: the original URL is kept so the
// entry persists with it; the local copy may remain on disk.
func TestMirrorStorageR2FailureKeepsOriginalURL(t *testing.T) {
	ms, origin, _ := newDualWriteStorage(t, http.StatusInternalServerError)

	obj := &Object{Filename: "abc.jpg", Url: origin.URL + "/abc.jpg"}
	originalURL := obj.Url
	_, err := ms.Mirror(obj)
	assert.Error(t, err)
	assert.Equal(t, originalURL, obj.Url)

	_, err = os.Stat(filepath.Join(ms.local.path, "a/b/c.jpg"))
	assert.NoError(t, err) // local copy may remain
}

// Without full R2 credentials NewStorage degrades to local-only
// mirroring.
func TestNewStorageDegradesWithoutR2Credentials(t *testing.T) {
	for _, cfg := range []*util.Config{
		{MediaPath: t.TempDir()},
		{MediaPath: t.TempDir(), R2AccountID: "acct"}, // partial credentials
	} {
		s := NewStorage(cfg, 640)
		ms, ok := s.(*MirrorStorage)
		assert.True(t, ok)
		assert.Nil(t, ms.r2)
	}

	// Local-only mirroring still works and leaves Bucket empty.
	s := NewStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	ms := s.(*MirrorStorage)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("local only"))
	}))
	defer origin.Close()
	ms.local.httpClient = origin.Client()

	obj, err := ms.FromUrl("", origin.URL+"/pic.jpg", "")
	assert.NoError(t, err)
	assert.Equal(t, "https://m.friendfeed.me/p/i/c.jpg", obj.Url)
	assert.Empty(t, obj.Bucket)

	_, err = os.Stat(filepath.Join(ms.local.path, "p/i/c.jpg"))
	assert.NoError(t, err)
}

// With full credentials NewStorage wires the R2 client.
func TestNewStorageWithR2Credentials(t *testing.T) {
	s := NewStorage(&util.Config{
		MediaPath:         t.TempDir(),
		R2AccountID:       "acct",
		R2AccessKeyID:     "ak",
		R2SecretAccessKey: "sk",
		R2Bucket:          "media",
	}, 640)
	ms, ok := s.(*MirrorStorage)
	assert.True(t, ok)
	if assert.NotNil(t, ms.r2) {
		assert.Equal(t, "media", ms.r2.bucket)
		assert.Equal(t, "https://acct.r2.cloudflarestorage.com", ms.r2.endpoint)
	}
}
