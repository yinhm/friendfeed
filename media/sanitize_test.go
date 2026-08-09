package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/util"
)

func TestSanitizeObjectKey(t *testing.T) {
	ok := map[string]string{
		"qq_logo_2x":      "qq_logo_2x",
		"path/to/pic.jpg": "path/to/pic.jpg",
		"a//b/./c":        "a/b/c",
		"weird name?.png": "weird_name_.png",
		`dir\file.jpg`:    "dir/file.jpg",
		"%2e%2e/literal":  "_2e_2e/literal", // percent decoding happens in url.Parse, not here
		".hidden/file..x": ".hidden/file..x",
		"a/" + ".." + "x": "a/..x", // only exact ".." segments are traversal
	}
	for in, want := range ok {
		got, err := sanitizeObjectKey(in)
		assert.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	empty := []string{"", ".", "./", "./."}
	for _, in := range empty {
		got, err := sanitizeObjectKey(in)
		assert.NoError(t, err, in)
		assert.Empty(t, got, in)
	}

	rejected := []string{
		"/etc/passwd",
		`/abs/win`,
		`C:\windows\system32`,
		"..",
		"../../outside",
		"a/../../b",
		`..\win`,
	}
	for _, in := range rejected {
		_, err := sanitizeObjectKey(in)
		assert.Error(t, err, in)
	}
}

// Untrusted file names must never escape the media root.
func TestPostRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	ms := NewLocalStorage(&util.Config{MediaPath: dir}, 640)

	for _, name := range []string{"../../outside", "/etc/target", `..\..\outside`, "a/../../b"} {
		obj := &Object{Filename: name, Content: []byte("x")}
		_, err := ms.Post(obj)
		assert.Error(t, err, name)
	}

	// Nothing was written outside the media root, and the media root stays
	// empty.
	_, err := os.Stat(filepath.Join(dir, "..", "..", "outside"))
	assert.True(t, errors.Is(err, os.ErrNotExist))
	entries, err := os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Empty(t, entries)
}

// URL paths are untrusted: traversal variants, including percent-encoded
// ones decoded by url.Parse, fail the mirror instead of writing outside
// the media root.
func TestFromUrlRejectsURLPathTraversal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	ms := NewLocalStorage(&util.Config{MediaPath: dir}, 640)
	ms.httpClient = srv.Client()

	for _, suffix := range []string{
		"/../../etc/target",
		"/%2e%2e/%2e%2e/etc/target",
		"/a/%2E%2E/b",
	} {
		_, err := ms.FromUrl("", srv.URL+suffix, "")
		assert.Error(t, err, suffix)
	}

	_, err := os.Stat(filepath.Join(dir, "..", "etc", "target"))
	assert.True(t, errors.Is(err, os.ErrNotExist))
	entries, err := os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Empty(t, entries)
}

// A name with no usable object key falls back to the content sha256.
func TestPostFallsBackToContentHash(t *testing.T) {
	dir := t.TempDir()
	ms := NewLocalStorage(&util.Config{MediaPath: dir}, 640)
	content := []byte("hash me")

	obj := &Object{Filename: ".", Content: content}
	_, err := ms.Post(obj)
	assert.NoError(t, err)

	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	assert.Equal(t, want, obj.Filename)
	assert.Equal(t, want[:1]+"/"+want[1:2]+"/"+want[2:], obj.Path)

	written, err := os.ReadFile(filepath.Join(dir, obj.Path))
	assert.NoError(t, err)
	assert.Equal(t, content, written)
}

// Without a usable name and without content there is nothing to key on.
func TestPostEmptyKeyAndContent(t *testing.T) {
	ms := NewLocalStorage(&util.Config{MediaPath: t.TempDir()}, 640)
	_, err := ms.Post(&Object{Filename: "."})
	assert.Error(t, err)
}

func TestContained(t *testing.T) {
	dir := t.TempDir()
	ms := NewLocalStorage(&util.Config{MediaPath: dir}, 640)

	assert.NoError(t, ms.contained(filepath.Join(dir, "a/b/c")))
	assert.Error(t, ms.contained(filepath.Join(dir, "..", "escape")))
	assert.Error(t, ms.contained(filepath.Join(dir, "..", "..", "escape")))
	assert.Error(t, ms.contained("/etc/passwd"))
}

func TestNewLocalStorageDefaultsMediaPath(t *testing.T) {
	ms := NewLocalStorage(&util.Config{DBPath: "/tmp/somedb"}, 640)
	assert.Equal(t, filepath.Join("/tmp/somedb", "files"), ms.path)

	ms = NewLocalStorage(&util.Config{DBPath: "/tmp/somedb", MediaPath: "/tmp/media"}, 640)
	assert.Equal(t, "/tmp/media", ms.path)
}
