package media

import (
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/util"
)

func TestMedia(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		if err := png.Encode(w, image.NewRGBA(image.Rect(0, 0, 1000, 100))); err != nil {
			t.Error(err)
		}
	}))
	defer imageServer.Close()

	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	// The default fetch client refuses loopback targets (SSRF guard);
	// tests serving content from httptest swap in a plain client.
	ms.httpClient = imageServer.Client()
	found, err := ms.Exists("not-exist-file")
	assert.NoError(t, err)
	assert.False(t, found)

	filename, fullpath := ms.shardFilepath("qq_logo_2x-640.jpg")
	assert.Equal(t, "q/q/_logo_2x-640.jpg", filename)
	assert.Equal(t, filepath.Join(ms.path, "q/q/_logo_2x-640.jpg"), fullpath)

	obj := &Object{
		Filename: "qq_logo_2x",
		Url:      imageServer.URL + "/image.png",
	}

	_, err = ms.Fetch(obj)
	assert.Nil(t, err)

	_, err = ms.Post(obj)
	assert.Nil(t, err)
	assert.Equal(t, "q/q/_logo_2x", obj.Path)

	found, err = ms.Exists(obj.Path)
	assert.Nil(t, err)
	assert.True(t, found)

	tObj, err := ms.Thumbnail(obj)
	assert.Nil(t, err)
	assert.Equal(t, "q/q/_logo_2x-640.jpg", tObj.Filename)
	assert.Equal(t, "q/q/_logo_2x-640.jpg", tObj.Path)
	assert.Equal(t, int32(640), tObj.Width)

}

func TestPostWritesNonExecutableFile(t *testing.T) {
	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	obj := &Object{Filename: "media-file", Content: []byte("content")}

	if _, err := ms.Post(obj); err != nil {
		t.Fatal(err)
	}
	_, fullPath := ms.shardFilepath(obj.Filename)
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0644); got != want {
		t.Fatalf("media file permissions = %o, want %o", got, want)
	}
}

func TestFetchNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	ms.httpClient = srv.Client()

	obj := &Object{Filename: "x.jpg", Url: srv.URL + "/x.jpg"}
	_, err := ms.Fetch(obj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Empty(t, obj.Content)
}

func TestFetchBodyTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(make([]byte, maxFetchBytes+1))
	}))
	defer srv.Close()

	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	ms.httpClient = srv.Client()

	obj := &Object{Filename: "big.bin", Url: srv.URL + "/big.bin"}
	_, err := ms.Fetch(obj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestFetchRedirectLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	// Keep the production CheckRedirect policy, but swap out the
	// SSRF-guarded transport so the loopback test server is reachable.
	client := newFetchClient()
	client.Transport = &http.Transport{}

	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	ms.httpClient = client

	obj := &Object{Filename: "r.jpg", Url: srv.URL + "/loop"}
	_, err := ms.Fetch(obj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redirect")
}

// The default client must refuse loopback/private/link-local targets; the
// dial fails before any connection is attempted.
func TestFetchRejectsNonPublicAddress(t *testing.T) {
	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)

	for _, rawURL := range []string{
		"http://127.0.0.1:1/secret",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/internal",
		"http://[::1]/",
	} {
		obj := &Object{Filename: "x", Url: rawURL}
		_, err := ms.Fetch(obj)
		assert.Error(t, err, rawURL)
	}
}

// The SSRF guard lives in DialContext, so it applies to the initial request
// and to every redirect hop alike.
func TestSafeDialContextRejectsNonPublicAddress(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:80",
		"169.254.169.254:80",
		"10.0.0.1:443",
		"192.168.1.1:443",
		"[::1]:80",
	} {
		_, err := safeDialContext(context.Background(), "tcp", addr)
		assert.Error(t, err, addr)
	}
}

func TestMirrorEndToEnd(t *testing.T) {
	content := []byte("fake image bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	ms.httpClient = srv.Client()

	obj := &Object{Filename: "abc.jpg", Url: srv.URL + "/abc.jpg"}
	got, err := ms.Mirror(obj)
	assert.NoError(t, err)
	assert.Equal(t, "a/b/c.jpg", got.Path)
	assert.Equal(t, "https://m.friendfeed.me/a/b/c.jpg", got.Url)
	assert.Equal(t, "image/png", got.MimeType)
	assert.Empty(t, got.Bucket)

	written, err := os.ReadFile(filepath.Join(ms.path, "a/b/c.jpg"))
	assert.NoError(t, err)
	assert.Equal(t, content, written)
}

// A failed mirror leaves obj.Url untouched so callers can persist the
// original URL.
func TestMirrorFailureKeepsOriginalURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	ms.httpClient = srv.Client()

	obj := &Object{Filename: "abc.jpg", Url: srv.URL + "/abc.jpg"}
	_, err := ms.Mirror(obj)
	assert.Error(t, err)
	assert.Equal(t, srv.URL+"/abc.jpg", obj.Url)

	found, err := ms.Exists("abc.jpg")
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestFromUrlMirrorsAndRewritesURL(t *testing.T) {
	content := []byte("pic")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	ms.httpClient = srv.Client()

	obj, err := ms.FromUrl("", srv.URL+"/path/to/pic.jpg", "")
	assert.NoError(t, err)
	assert.Equal(t, "path/to/pic.jpg", obj.Path)
	assert.Equal(t, "https://m.friendfeed.me/path/to/pic.jpg", obj.Url)
	assert.Equal(t, "image/jpeg", obj.MimeType)

	written, err := os.ReadFile(filepath.Join(ms.path, "path/to/pic.jpg"))
	assert.NoError(t, err)
	assert.Equal(t, content, written)

	// An explicit filename and mimetype win over the URL path and header.
	obj, err = ms.FromUrl("wxyz", srv.URL+"/path/to/pic.jpg", "application/pdf")
	assert.NoError(t, err)
	assert.Equal(t, "w/x/yz", obj.Path)
	assert.Equal(t, "https://m.friendfeed.me/w/x/yz", obj.Url)
	assert.Equal(t, "application/pdf", obj.MimeType)
}

func TestNewLocalStorageMediaBaseURL(t *testing.T) {
	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	assert.Equal(t, defaultMediaBaseURL, ms.mediaBaseURL)

	ms = NewLocalStorage(&util.Config{
		MediaPath: t.TempDir(),
		MediaURL:  "https://cdn.example.com/",
	}, 640)
	assert.Equal(t, "https://cdn.example.com", ms.mediaBaseURL)
}
