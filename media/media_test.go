package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/util"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
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
	wantPath := contentObjectKey(obj.Content)
	assert.Equal(t, wantPath, obj.Path)

	found, err = ms.Exists(obj.Path)
	assert.Nil(t, err)
	assert.True(t, found)

	tObj, err := ms.Thumbnail(obj)
	assert.Nil(t, err)
	assert.Equal(t, wantPath+"-640.jpg", tObj.Filename)
	assert.Equal(t, wantPath+"-640.jpg", tObj.Path)
	assert.Equal(t, int32(640), tObj.Width)

}

// Thumbnail must keep accepting every format imaging.Open handled before the
// switch to bild: JPEG, PNG, GIF, TIFF and BMP.
func TestThumbnailDecodesLegacyFormats(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 100))
	encoders := map[string]func(io.Writer, image.Image) error{
		"jpeg": func(w io.Writer, m image.Image) error {
			return jpeg.Encode(w, m, nil)
		},
		"png":  func(w io.Writer, m image.Image) error { return png.Encode(w, m) },
		"gif":  func(w io.Writer, m image.Image) error { return gif.Encode(w, m, nil) },
		"tiff": func(w io.Writer, m image.Image) error { return tiff.Encode(w, m, nil) },
		"bmp":  func(w io.Writer, m image.Image) error { return bmp.Encode(w, m) },
	}

	for format, encode := range encoders {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			if err := encode(&buf, src); err != nil {
				t.Fatal(err)
			}

			ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
			obj := &Object{Filename: "upload." + format, Content: buf.Bytes()}
			if _, err := ms.Post(obj); err != nil {
				t.Fatal(err)
			}

			tObj, err := ms.Thumbnail(obj)
			if err != nil {
				t.Fatalf("Thumbnail(%s): %v", format, err)
			}
			assert.Equal(t, int32(640), tObj.Width)
			assert.Equal(t, int32(64), tObj.Height)
			assert.Equal(t, "image/jpeg", tObj.MimeType)
		})
	}
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

func TestWriteFileAtomicNeverExposesPartialContent(t *testing.T) {
	fullPath := filepath.Join(t.TempDir(), "media-file")
	first := bytes.Repeat([]byte("a"), 1<<20)
	second := bytes.Repeat([]byte("b"), 1<<20)
	if err := writeFileAtomic(fullPath, first, 0644); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			got, err := os.ReadFile(fullPath)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			if !bytes.Equal(got, first) && !bytes.Equal(got, second) {
				select {
				case errCh <- fmt.Errorf("read partial media file of %d bytes", len(got)):
				default:
				}
				return
			}
		}
	}()

	for i := 0; i < 20; i++ {
		content := first
		if i%2 != 0 {
			content = second
		}
		if err := writeFileAtomic(fullPath, content, 0644); err != nil {
			close(done)
			wg.Wait()
			t.Fatal(err)
		}
	}
	close(done)
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestWriteFileAtomicRemovesTemporaryFileOnPublishFailure(t *testing.T) {
	dir := t.TempDir()
	fullPath := filepath.Join(dir, "existing-directory")
	if err := os.Mkdir(fullPath, 0755); err != nil {
		t.Fatal(err)
	}

	if err := writeFileAtomic(fullPath, []byte("content"), 0644); err == nil {
		t.Fatal("writeFileAtomic succeeded with a directory as destination")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".media-tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary media files remain after failure: %v", matches)
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
		"100.64.0.1:80",
		"100.127.255.254:80",
		"[::1]:80",
	} {
		_, err := safeDialContext(context.Background(), "tcp", addr)
		assert.Error(t, err, addr)
	}
}

func TestIsCGNATBoundaries(t *testing.T) {
	for _, rawIP := range []string{"100.64.0.0", "100.127.255.255"} {
		assert.True(t, isCGNAT(net.ParseIP(rawIP)), rawIP)
	}
	for _, rawIP := range []string{"100.63.255.255", "100.128.0.0", "2001:db8::1"} {
		assert.False(t, isCGNAT(net.ParseIP(rawIP)), rawIP)
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
	wantPath := contentObjectKey(content)
	assert.Equal(t, wantPath, got.Path)
	assert.Equal(t, "https://m.friendfeed.me/"+wantPath, got.Url)
	assert.Equal(t, "image/png", got.MimeType)
	assert.Empty(t, got.Bucket)

	written, err := os.ReadFile(filepath.Join(ms.path, wantPath))
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
	wantPath := contentObjectKey(content)
	assert.Equal(t, wantPath, obj.Path)
	assert.Equal(t, "https://m.friendfeed.me/"+wantPath, obj.Url)
	assert.Equal(t, "image/jpeg", obj.MimeType)

	written, err := os.ReadFile(filepath.Join(ms.path, wantPath))
	assert.NoError(t, err)
	assert.Equal(t, content, written)

	// An explicit filename and mimetype win over the URL path and header.
	obj, err = ms.FromUrl("wxyz", srv.URL+"/path/to/pic.jpg", "application/pdf")
	assert.NoError(t, err)
	wantPath = contentObjectKey(content)
	assert.Equal(t, wantPath, obj.Path)
	assert.Equal(t, "https://m.friendfeed.me/"+wantPath, obj.Url)
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
