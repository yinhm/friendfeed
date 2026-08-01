package media

import (
	"log"
	"net/http"

	"github.com/yinhm/friendfeed/util"
)

// MirrorStorage is the production media storage: Post writes the object to
// the local media directory (sharded paths) and, when R2 credentials are
// configured, also PUTs it to the R2 bucket under the same object key.
// Url points at the public media base URL (media_url, default
// https://m.friendfeed.me) fronting the bucket, and Bucket finally carries
// the R2 bucket name it was reserved for.
//
// An R2 failure makes the whole Post fail: the local copy may remain on
// disk, but callers treat the mirror as failed and keep the original URL.
type MirrorStorage struct {
	local *LocalStorage
	r2    *R2Client // nil: local-only mirroring
}

// NewStorage builds the Storage used on the archive path. With full R2
// credentials it dual-writes local + R2; otherwise it logs once and
// degrades to local-only mirroring so development and tests keep working.
func NewStorage(cfg *util.Config, maxWidth int) Storage {
	local := NewLocalStorage(cfg, maxWidth)
	if !r2Configured(cfg) {
		log.Println("media: R2 not configured, mirroring to local storage only")
		return &MirrorStorage{local: local}
	}
	return &MirrorStorage{local: local, r2: newR2Client(cfg)}
}

func (m *MirrorStorage) Exists(name string) (bool, error) { return m.local.Exists(name) }

func (m *MirrorStorage) Fetch(obj *Object) (*http.Response, error) { return m.local.Fetch(obj) }

func (m *MirrorStorage) Thumbnail(obj *Object) (*Object, error) { return m.local.Thumbnail(obj) }

// Post writes the object locally first, then uploads it to R2.
func (m *MirrorStorage) Post(obj *Object) (*Object, error) {
	if _, err := m.local.Post(obj); err != nil {
		return obj, err
	}
	if m.r2 != nil {
		if err := m.r2.Put(obj.Path, obj.Content, obj.MimeType); err != nil {
			return obj, err
		}
		obj.Bucket = m.r2.bucket
	}
	return obj, nil
}

// Mirror fetches the remote object and stores it via Post, then rewrites
// obj.Url to the mirrored address (<mediaBaseURL>/<sharded path>).
func (m *MirrorStorage) Mirror(obj *Object) (*Object, error) {
	if _, err := m.local.Fetch(obj); err != nil {
		return nil, err
	}
	if _, err := m.Post(obj); err != nil {
		return nil, err
	}
	obj.Url = m.local.mediaBaseURL + "/" + obj.Path
	return obj, nil
}

func (m *MirrorStorage) FromUrl(filename, src, mimetype string) (*Object, error) {
	obj, err := objectFromUrl(filename, src, mimetype)
	if err != nil {
		return nil, err
	}
	return m.Mirror(obj)
}
