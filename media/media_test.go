package media

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/util"
)

func TestMedia(t *testing.T) {
	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	assert.Nil(t, err)

	ms := NewLocalStorage(cfg)
	found, err := ms.Exists("not-exist-file")
	assert.NotNil(t, err)
	assert.False(t, found)

	obj := &Object{
		Filename: "qq_logo_2x",
		Url:      "https://mat1.gtimg.com/pingjs/ext2020/qqindex2018/dist/img/qq_logo_2x.png",
	}

	_, err = ms.Fetch(obj)
	assert.Nil(t, err)

	_, err = ms.Post(obj)
	assert.Nil(t, err)

	found, err = ms.Exists(obj.Path)
	assert.Nil(t, err)
	assert.True(t, found)

	filename, err := ms.Thumbnail(obj)
	assert.Nil(t, err)
	assert.Equal(t, "q/q/_logo_2x-640.jpg", filename)

	os.RemoveAll(ms.path)
}
