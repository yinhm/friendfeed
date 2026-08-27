package media

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/yinhm/friendfeed/util"
)

// PublicURL returns the canonical public URL for a stored object path.
func PublicURL(cfg *util.Config, objectPath string) string {
	base := defaultMediaBaseURL
	if cfg != nil && cfg.MediaURL != "" {
		base = strings.TrimRight(cfg.MediaURL, "/")
	}
	return base + "/" + strings.TrimLeft(objectPath, "/")
}

// MirrorStorage is the production media storage: Post writes the object to
// the local media directory (sharded paths) and, when R2 credentials are
// configured, also PUTs it to the R2 bucket under the same object key.
// Url points at the public media base URL (media_url, default
// https://m.friendfeed.me) which fronts both the bucket and the local
// media directory, and Bucket finally carries the R2 bucket name it was
// reserved for.
//
// An R2 failure makes the whole Post fail: the local copy may remain on
// disk, but callers treat the mirror as failed and keep the original URL.
type MirrorStorage struct {
	local *LocalStorage
	r2    *R2Client // nil: local-only mirroring
	// r2Err is non-nil when the R2 config is partial (some but not all of the
	// four fields set). Mirroring must fail loudly in that case: silently
	// degrading to local-only would persist public-domain URLs for objects
	// that were never uploaded to R2.
	r2Err error
}

// NewStorage builds the Storage used on the archive path. R2 dual-write
// requires both the media_mirror switch and full R2 credentials; otherwise
// it logs once and runs in explicit local-only mode so development and tests
// keep working. A partial R2 config with media_mirror on returns a storage
// whose Mirror/Post fail (keeping original URLs) instead of silently
// degrading.
func NewStorage(cfg *util.Config, maxWidth int) Storage {
	local := NewLocalStorage(cfg, maxWidth)
	if !cfg.MediaMirror {
		log.Println("media: media_mirror disabled, mirroring to local storage only")
		return &MirrorStorage{local: local}
	}
	switch n := r2ConfigCount(cfg); n {
	case 0:
		log.Println("media: R2 not configured, mirroring to local storage only")
		return &MirrorStorage{local: local}
	case 4:
		return &MirrorStorage{local: local, r2: newR2Client(cfg)}
	default:
		err := fmt.Errorf("media: partial R2 configuration (%d/4 fields set), mirroring disabled", n)
		log.Println(err)
		return &MirrorStorage{local: local, r2Err: err}
	}
}

func (m *MirrorStorage) Exists(name string) (bool, error) { return m.local.Exists(name) }

func (m *MirrorStorage) Fetch(obj *Object) (*http.Response, error) { return m.local.Fetch(obj) }

func (m *MirrorStorage) Thumbnail(obj *Object) (*Object, error) { return m.local.Thumbnail(obj) }

// Post writes the object locally first, then uploads it to R2.
func (m *MirrorStorage) Post(obj *Object) (*Object, error) {
	if m.r2Err != nil {
		return obj, m.r2Err
	}
	if _, err := m.local.Post(obj); err != nil {
		return obj, err
	}
	if m.r2 != nil {
		// MimeType may originate in an archived RPC payload. Derive the public
		// object's header from its bytes instead of trusting that metadata.
		contentType := http.DetectContentType(obj.Content)
		if err := m.r2.Put(obj.Path, obj.Content, contentType); err != nil {
			return obj, err
		}
		obj.Bucket = m.r2.bucket
	}
	return obj, nil
}

// Mirror fetches the remote object and stores it via Post, then rewrites
// obj.Url to the mirrored address (<mediaBaseURL>/<sharded path>).
func (m *MirrorStorage) Mirror(obj *Object) (*Object, error) {
	if m.r2Err != nil {
		return nil, m.r2Err
	}
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
