package media

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/yinhm/friendfeed/util"
)

// type thumbConfig struct {
// 	Width  int    `json:"width"`
// 	Height int    `json:"height"`
// 	Shape  string `json:"shape"`
// }

type Object struct {
	Filename string
	Bucket   string
	Path     string
	MimeType string
	Url      string
	Content  []byte

	// for image
	Width  int32
	Height int32
}

type Storage interface {
	Exists(name string) (bool, error)
	Fetch(obj *Object) (*http.Response, error)
	Post(obj *Object) (*Object, error)
	Thumbnail(obj *Object) (*Object, error)
	Mirror(obj *Object) (*Object, error)
	FromUrl(filename, src, mimetype string) (*Object, error)
}

type LocalStorage struct {
	path     string
	maxWidth int
}

func NewLocalStorage(cfg *util.Config, maxWidth int) *LocalStorage {
	ls := &LocalStorage{
		path:     cfg.MediaPath,
		maxWidth: maxWidth,
	}
	return ls
}

func (c *LocalStorage) shardFilepath(filename string) (string, string) {
	if len(filename) <= 3 ||
		filepath.Dir(filename) != "." {
		return filename, filepath.Join(c.path, filename)
	}

	outFile := filename[:2] + "/" + filename[2:]
	outFile = outFile[:1] + "/" + outFile[1:]
	return outFile, filepath.Join(c.path, outFile)
}

func (c *LocalStorage) Exists(name string) (bool, error) {
	_, fullPath := c.shardFilepath(name)
	// filepath := filepath.Join(c.path, name)
	if _, err := os.Stat(fullPath); err != nil {
		return false, err
	}
	return true, nil
}

func (c *LocalStorage) Mirror(obj *Object) (*Object, error) {
	return nil, fmt.Errorf("Mirror not implemented yet: %s", obj.Url)
}

func (c *LocalStorage) FromUrl(filename, src, mimetype string) (*Object, error) {
	parsed, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("can not parse: %s", src)
	}
	newpath := strings.TrimLeft(parsed.Path, "/")
	if filename == "" {
		filename = newpath
	}
	obj := &Object{
		Filename: filename,
		Path:     newpath,
		Url:      src,
	}
	if mimetype != "" {
		obj.MimeType = mimetype
	}

	return c.Mirror(obj)
}

// write object file to disk
func (c *LocalStorage) Post(obj *Object) (*Object, error) {
	outFile, fullPath := c.shardFilepath(obj.Filename)

	os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err := os.WriteFile(fullPath, obj.Content, 0755); err != nil {
		return obj, err
	}

	obj.Path = outFile
	return obj, nil
}

// fetch file from url
func (c *LocalStorage) Fetch(obj *Object) (*http.Response, error) {
	resp, err := http.Get(obj.Url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return resp, err
	}

	mimeType := resp.Header.Get("Content-Type")
	// contentDisposition := resp.Header.Get("Content-Disposition")
	if obj.MimeType == "" {
		obj.MimeType = mimeType
	}

	obj.Content = body
	return resp, nil
}

// Thumbnail resize the image to width=640px while preserving the aspect ratio.
func (c *LocalStorage) Thumbnail(obj *Object) (*Object, error) {
	thumbSuffix := fmt.Sprintf("-%d.jpg", c.maxWidth)

	fullpath := filepath.Join(c.path, obj.Path)
	fromImage, err := imaging.Open(fullpath)
	if err != nil {
		return nil, fmt.Errorf("error while open image: %s", err)
	}

	obj.Width = int32(fromImage.Bounds().Dx())
	obj.Height = int32(fromImage.Bounds().Dy())

	// resize limit to src image too large
	if obj.Width <= int32(float64(c.maxWidth)*1.3) {
		return obj, nil
	}

	dst := imaging.Resize(fromImage, c.maxWidth, 0, imaging.Lanczos)
	dstFilepath := fullpath + thumbSuffix
	// imaging.Save guest image format from extension
	if err := imaging.Save(dst, dstFilepath); err != nil {
		return nil, fmt.Errorf("error while saving image: %s", err)
	}

	thumbObj := &Object{
		Filename: obj.Path + thumbSuffix,
		Path:     obj.Path + thumbSuffix,
		MimeType: "image/jpeg",
		Width:    int32(dst.Rect.Size().X),
		Height:   int32(dst.Rect.Size().Y),
	}
	return thumbObj, nil
}
