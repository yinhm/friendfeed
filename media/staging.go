package media

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yinhm/friendfeed/util"
)

const StagingDirectory = "upload-staging"

// StagingStore owns temporary browser uploads. It is deliberately separate
// from Storage: staged objects are local-only and must never be dual-written
// to R2 before an Entry actually references them.
type StagingStore struct {
	root string
}

func CanonicalKeyFromURL(mediaBaseURL, raw string) (string, bool) {
	base, err := url.Parse(strings.TrimRight(mediaBaseURL, "/"))
	if err != nil {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != base.Scheme || parsed.Host != base.Host {
		return "", false
	}
	key := strings.TrimPrefix(parsed.Path, strings.TrimRight(base.Path, "/")+"/")
	parts := strings.Split(key, "/")
	if len(parts) != 3 || len(parts[0]) != 1 || len(parts[1]) != 1 {
		return "", false
	}
	last := parts[2]
	dot := strings.LastIndexByte(last, '.')
	if dot <= 0 {
		return "", false
	}
	digest := parts[0] + parts[1] + last[:dot]
	if len(digest) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", false
	}
	return key, true
}

func NewStagingStore(cfg *util.Config) *StagingStore {
	root := cfg.MediaPath
	if root == "" {
		root = filepath.Join(cfg.DBPath, "files")
	}
	return &StagingStore{root: root}
}

func (s *StagingStore) Put(content []byte, extension string) (string, string, error) {
	extension = strings.TrimPrefix(strings.ToLower(extension), ".")
	if extension == "" || strings.ContainsAny(extension, `/\\`) {
		return "", "", errors.New("media: invalid staging extension")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	id := hex.EncodeToString(random)
	name := id + "." + extension
	dir := filepath.Join(s.root, StagingDirectory)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	if err := writeFileAtomic(filepath.Join(dir, name), content, 0644); err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(content)
	return name, hex.EncodeToString(digest[:]), nil
}

// Promote verifies staged bytes against signed metadata and atomically
// publishes the content-addressed canonical object. Existing content is
// reused. The staging file is left for the TTL cleaner, making retries safe.
func (s *StagingStore) Promote(name, digest, extension string, size int) (string, error) {
	extension = strings.TrimPrefix(strings.ToLower(extension), ".")
	wantSuffix := "." + extension
	id := strings.TrimSuffix(name, wantSuffix)
	if filepath.Base(name) != name || !strings.HasSuffix(name, wantSuffix) || len(id) != 32 {
		return "", errors.New("media: invalid staging object")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", errors.New("media: invalid staging object")
	}
	content, err := os.ReadFile(filepath.Join(s.root, StagingDirectory, name))
	if err != nil {
		return "", err
	}
	if len(content) != size {
		return "", errors.New("media: staged object size changed")
	}
	sum := sha256.Sum256(content)
	actual := hex.EncodeToString(sum[:])
	if actual != digest {
		return "", errors.New("media: staged object digest changed")
	}
	key := actual[:1] + "/" + actual[1:2] + "/" + actual[2:] + "." + extension
	full := filepath.Join(s.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return "", err
	}
	if _, err := os.Stat(full); err == nil {
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := writeFileAtomic(full, content, 0644); err != nil {
		return "", err
	}
	return key, nil
}

func (s *StagingStore) Cleanup(now time.Time, ttl time.Duration) (int, error) {
	dir := filepath.Join(s.root, StagingDirectory)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, fmt.Errorf("media: inspect staging object: %w", err)
		}
		if now.Sub(info.ModTime()) <= ttl {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("media: remove staging object: %w", err)
		}
		removed++
	}
	return removed, nil
}
