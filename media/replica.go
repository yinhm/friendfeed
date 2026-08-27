package media

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yinhm/friendfeed/util"
)

// R2Replica copies already-published local canonical objects to R2. It is
// intentionally separate from Storage.Post so browser publish does not wait
// for the remote replica.
type R2Replica struct {
	root string
	r2   *R2Client
}

func NewR2Replica(cfg *util.Config) (*R2Replica, error) {
	if !cfg.MediaMirror {
		return nil, nil
	}
	switch r2ConfigCount(cfg) {
	case 0:
		return nil, nil
	case 4:
		root := cfg.MediaPath
		if root == "" {
			root = filepath.Join(cfg.DBPath, "files")
		}
		return &R2Replica{root: root, r2: newR2Client(cfg)}, nil
	default:
		return nil, errors.New("media: partial R2 configuration")
	}
}

func (r *R2Replica) PutLocal(key, mimeType string) error {
	clean, err := sanitizeObjectKey(key)
	if err != nil || clean != key || clean == "" {
		return errors.New("media: invalid canonical object key")
	}
	full := filepath.Join(r.root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(r.root, full)
	if err != nil || rel == ".." || filepath.IsAbs(rel) {
		return errors.New("media: canonical object escapes root")
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("media: read canonical object: %w", err)
	}
	if len(content) > MaxUploadFileBytes {
		return errors.New("media: canonical upload object exceeds limit")
	}
	return r.r2.Put(clean, content, mimeType)
}
