package media

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/util"
)

func TestMedia(t *testing.T) {
	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	assert.Nil(t, err)

	ms := NewLocalStorage(cfg, 640)
	found, err := ms.Exists("not-exist-file")
	assert.NotNil(t, err)
	assert.False(t, found)

	filename, fullpath := ms.shardFilepath("qq_logo_2x-640.jpg")
	assert.Equal(t, "q/q/_logo_2x-640.jpg", filename)
	assert.Equal(t, filepath.Join(ms.path, "q/q/_logo_2x-640.jpg"), fullpath)

	obj := &Object{
		Filename: "qq_logo_2x",
		Url:      "https://mat1.gtimg.com/pingjs/ext2020/qqindex2018/dist/img/qq_logo_2x.png",
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

	os.RemoveAll(ms.path)
}
