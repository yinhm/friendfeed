package media

import (
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
