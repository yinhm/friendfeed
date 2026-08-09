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
	path        string
	body        []byte
	contentType string
}

func (r *r2Recorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.puts = append(r.puts, recordedPut{
			path:        req.URL.Path,
			body:        body,
			contentType: req.Header.Get("Content-Type"),
		})
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
	content := []byte("dual write image bytes")
	wantPath := contentObjectKey(content)
	assert.Equal(t, wantPath, obj.Path)
	assert.Equal(t, "https://m.friendfeed.me/"+wantPath, obj.Url)
	assert.Equal(t, "media", obj.Bucket)

	written, err := os.ReadFile(filepath.Join(ms.local.path, wantPath))
	assert.NoError(t, err)
	assert.Equal(t, content, written)

	puts := recorder.recorded()
	assert.Len(t, puts, 1)
	assert.Equal(t, "/media/"+wantPath, puts[0].path)
	assert.Equal(t, content, puts[0].body)
	assert.Equal(t, "text/plain; charset=utf-8", puts[0].contentType)
}

func TestMirrorStorageDoesNotTrustSubmittedContentType(t *testing.T) {
	ms, _, recorder := newDualWriteStorage(t, http.StatusOK)
	content := []byte("\x89PNG\r\n\x1a\n")
	obj := &Object{
		Filename: "image.png",
		MimeType: "text/html",
		Content:  content,
	}

	_, err := ms.Post(obj)
	assert.NoError(t, err)
	puts := recorder.recorded()
	assert.Len(t, puts, 1)
	assert.Equal(t, "image/png", puts[0].contentType)
	assert.Equal(t, "text/html", obj.MimeType, "storage object contract remains unchanged")
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

	wantPath := contentObjectKey([]byte("dual write image bytes"))
	_, err = os.Stat(filepath.Join(ms.local.path, wantPath))
	assert.NoError(t, err) // local copy may remain
}

// Without any R2 credentials NewStorage runs in explicit local-only mode.
func TestNewStorageLocalOnlyWithoutR2(t *testing.T) {
	s := NewStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	ms, ok := s.(*MirrorStorage)
	assert.True(t, ok)
	assert.Nil(t, ms.r2)
	assert.NoError(t, ms.r2Err)

	// Local-only mirroring still works and leaves Bucket empty.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("local only"))
	}))
	defer origin.Close()
	ms.local.httpClient = origin.Client()

	obj, err := ms.FromUrl("", origin.URL+"/pic.jpg", "")
	assert.NoError(t, err)
	wantPath := contentObjectKey([]byte("local only"))
	assert.Equal(t, "https://m.friendfeed.me/"+wantPath, obj.Url)
	assert.Empty(t, obj.Bucket)

	_, err = os.Stat(filepath.Join(ms.local.path, wantPath))
	assert.NoError(t, err)
}

// A partial R2 config (some but not all fields set) is a configuration
// error: Mirror/Post must fail so entries keep their original URLs instead
// of persisting public-domain URLs for objects never uploaded to R2.
func TestNewStoragePartialR2ConfigFails(t *testing.T) {
	partials := []*util.Config{
		{MediaPath: t.TempDir(), R2AccountID: "acct"},
		{MediaPath: t.TempDir(), R2AccessKeyID: "ak", R2SecretAccessKey: "sk"},
		{MediaPath: t.TempDir(), R2AccountID: "acct", R2AccessKeyID: "ak", R2SecretAccessKey: "sk"},
	}
	for _, cfg := range partials {
		s := NewStorage(cfg, 640)
		ms, ok := s.(*MirrorStorage)
		assert.True(t, ok)
		assert.Nil(t, ms.r2)
		if assert.Error(t, ms.r2Err) {
			assert.Contains(t, ms.r2Err.Error(), "partial R2 configuration")
		}

		obj := &Object{Filename: "pic.jpg", Url: "https://example.com/pic.jpg"}
		originalURL := obj.Url
		_, err := ms.Mirror(obj)
		assert.Error(t, err)
		assert.Equal(t, originalURL, obj.Url, "failed mirror must keep the original URL")

		_, err = ms.Post(&Object{Filename: "pic.jpg", Content: []byte("x")})
		assert.Error(t, err)

		// The local directory stays empty: nothing is written or rewritten.
		entries, err := os.ReadDir(ms.local.path)
		assert.NoError(t, err)
		assert.Empty(t, entries)
	}
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
