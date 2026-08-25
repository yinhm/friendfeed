package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register decode formats imaging.Open used to accept
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"

	"github.com/anthonynsimon/bild/imgio"
	"github.com/anthonynsimon/bild/transform"
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

const (
	// defaultMediaBaseURL is the public domain mirrored objects are served
	// from (<mediaBaseURL>/<sharded path>): both the R2 bucket and the local
	// media directory are served under this same domain. Overridable via the
	// media_url config key.
	defaultMediaBaseURL = "https://m.friendfeed.me"

	fetchTimeout      = 30 * time.Second
	maxFetchBytes     = 32 << 20 // 32MB
	maxFetchRedirects = 10
)

type LocalStorage struct {
	path         string
	maxWidth     int
	mediaBaseURL string
	httpClient   *http.Client
}

func NewLocalStorage(cfg *util.Config, maxWidth int) *LocalStorage {
	path := cfg.MediaPath
	if path == "" {
		// Project convention: media files live next to the database.
		path = filepath.Join(cfg.DBPath, "files")
	}
	ls := &LocalStorage{
		path:         path,
		maxWidth:     maxWidth,
		mediaBaseURL: defaultMediaBaseURL,
		httpClient:   newFetchClient(),
	}
	if cfg.MediaURL != "" {
		ls.mediaBaseURL = strings.TrimRight(cfg.MediaURL, "/")
	}
	return ls
}

// newFetchClient builds the HTTP client used to mirror remote media: it has
// an overall timeout, follows a bounded number of redirects, and refuses to
// connect to loopback/private/link-local addresses (SSRF guard). The guard
// lives in DialContext, so it is enforced on every redirect hop as well.
func newFetchClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = safeDialContext
	return &http.Client{
		Timeout:   fetchTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFetchRedirects {
				return fmt.Errorf("stopped after %d redirects", maxFetchRedirects)
			}
			return nil
		},
	}
}

// safeDialContext resolves the target host and refuses to dial
// loopback/private/link-local addresses. It dials the resolved IP directly,
// so a hostname cannot be re-resolved to a different address afterwards
// (DNS rebinding).
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no such host: %s", host)
	}
	for _, ipa := range ips {
		if !isPublicIP(ipa.IP) {
			return nil, fmt.Errorf("refusing to dial non-public address %s for host %s", ipa.IP, host)
		}
	}
	dialer := &net.Dialer{Timeout: fetchTimeout}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

func isPublicIP(ip net.IP) bool {
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() &&
		!isCGNAT(ip)
}

func isCGNAT(ip net.IP) bool {
	ipv4 := ip.To4()
	return ipv4 != nil && ipv4[0] == 100 && ipv4[1] >= 64 && ipv4[1] <= 127
}

func (c *LocalStorage) shardFilepath(filename string) (string, string) {
	if len(filename) <= 3 ||
		filepath.Dir(filename) != "." {
		return filename, filepath.Join(c.path, filename)
	}

	outFile := filepath.Join(filename[:1], filename[1:2], filename[2:])
	return outFile, filepath.Join(c.path, outFile)
}

// sanitizeObjectKey turns an untrusted name (a remote URL path or an
// RPC-submitted file name) into a safe object key: backslashes are treated
// as separators, empty and "." segments are dropped, and every remaining
// segment is reduced to [A-Za-z0-9._-] with anything else replaced by "_".
//
// Absolute paths and ".." traversal segments are rejected with an error;
// untrusted input must never become a filesystem path directly. The result
// may be empty when the input carries no usable name; callers then fall
// back to a content hash as the key.
func sanitizeObjectKey(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, `/`) ||
		strings.HasPrefix(name, `\`) || (len(name) > 1 && name[1] == ':') {
		return "", fmt.Errorf("media: absolute object key %q rejected", name)
	}
	name = strings.ReplaceAll(name, `\`, "/")
	segments := strings.Split(name, "/")
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch segment {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("media: object key %q contains path traversal", name)
		}
		if cleaned := sanitizeKeySegment(segment); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return strings.Join(out, "/"), nil
}

func sanitizeKeySegment(segment string) string {
	var b strings.Builder
	b.Grow(len(segment))
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// contained verifies that a resolved path stays inside the media root.
// It is the final guard before anything is written to disk.
func (c *LocalStorage) contained(fullPath string) error {
	rel, err := filepath.Rel(c.path, filepath.Clean(fullPath))
	if err != nil {
		return err
	}
	if rel == ".." || filepath.IsAbs(rel) ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("media: path %q escapes media root %q", fullPath, c.path)
	}
	return nil
}

func (c *LocalStorage) Exists(name string) (bool, error) {
	_, fullPath := c.shardFilepath(name)
	// filepath := filepath.Join(c.path, name)
	if _, err := os.Stat(fullPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Mirror fetches the remote object and stores it locally, then rewrites
// obj.Url to the mirrored address (<mediaBaseURL>/<sharded path>) and keeps
// Path/Filename/MimeType in sync with the stored copy. obj.Bucket is left
// untouched: LocalStorage has no bucket concept, the field is reserved for
// S3/R2 backends.
func (c *LocalStorage) Mirror(obj *Object) (*Object, error) {
	if _, err := c.Fetch(obj); err != nil {
		return nil, err
	}
	if _, err := c.Post(obj); err != nil {
		return nil, err
	}
	obj.Url = c.mediaBaseURL + "/" + obj.Path
	return obj, nil
}

func (c *LocalStorage) FromUrl(filename, src, mimetype string) (*Object, error) {
	obj, err := objectFromUrl(filename, src, mimetype)
	if err != nil {
		return nil, err
	}
	return c.Mirror(obj)
}

// objectFromUrl builds an Object from a remote URL. The URL path is
// untrusted input: it only becomes an object key after the sanitization
// and containment checks in Post.
func objectFromUrl(filename, src, mimetype string) (*Object, error) {
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

	return obj, nil
}

// write object file to disk
//
// obj.Filename is untrusted input and is validated for compatibility, but it
// is not part of the stored key. Objects are addressed only by their full
// content sha256, split across two directory levels so local storage does not
// accumulate an unbounded number of entries in one directory. The resolved
// path is verified to stay inside the media root before writing.
func (c *LocalStorage) Post(obj *Object) (*Object, error) {
	filename, err := sanitizeObjectKey(obj.Filename)
	if err != nil {
		return obj, err
	}
	if filename == "" {
		if len(obj.Content) == 0 {
			return obj, fmt.Errorf("media: no usable object key for %q", obj.Filename)
		}
	}
	filename = contentObjectKey(obj.Content)
	obj.Filename = filename

	outFile, fullPath := c.shardFilepath(filename)
	if err := c.contained(fullPath); err != nil {
		return obj, err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return obj, err
	}
	if err := writeFileAtomic(fullPath, obj.Content, 0644); err != nil {
		return obj, err
	}

	obj.Path = outFile
	return obj, nil
}

// writeFileAtomic publishes a complete file in one rename. The temporary file
// lives beside the destination so readers can observe either the previous
// complete file or the new complete file, never an in-place partial write.
func writeFileAtomic(fullPath string, content []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(fullPath), ".media-tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary media file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	n, err := tmp.Write(content)
	if err != nil {
		return fmt.Errorf("write temporary media file: %w", err)
	}
	if n != len(content) {
		return fmt.Errorf("write temporary media file: %w", io.ErrShortWrite)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("set media file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary media file: %w", err)
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		return fmt.Errorf("publish media file: %w", err)
	}
	return nil
}

func contentObjectKey(content []byte) string {
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	return digest[:1] + "/" + digest[1:2] + "/" + digest[2:]
}

// fetch file from url
//
// Uses the storage's controlled client (timeout, redirect limit, SSRF
// guard). Only 2xx responses are accepted and the body is capped at
// maxFetchBytes.
func (c *LocalStorage) Fetch(obj *Object) (*http.Response, error) {
	resp, err := c.httpClient.Get(obj.Url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp, fmt.Errorf("fetch %s: unexpected status %s", obj.Url, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return resp, err
	}
	if len(body) > maxFetchBytes {
		return resp, fmt.Errorf("fetch %s: body exceeds %d bytes limit", obj.Url, maxFetchBytes)
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
	f, err := os.Open(fullpath)
	if err != nil {
		return nil, fmt.Errorf("error while open image: %w", err)
	}
	defer f.Close()
	fromImage, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("error while open image: %w", err)
	}

	obj.Width = int32(fromImage.Bounds().Dx())
	obj.Height = int32(fromImage.Bounds().Dy())

	// resize limit to src image too large
	if obj.Width <= int32(float64(c.maxWidth)*1.3) {
		return obj, nil
	}

	// bild treats a zero dimension as an empty image instead of preserving
	// the aspect ratio, so compute the target height explicitly.
	srcW, srcH := fromImage.Bounds().Dx(), fromImage.Bounds().Dy()
	height := int(float64(srcH) * float64(c.maxWidth) / float64(srcW))
	if height < 1 {
		height = 1
	}
	dst := transform.Resize(fromImage, c.maxWidth, height, transform.Lanczos)
	dstFilepath := fullpath + thumbSuffix
	if err := imgio.Save(dstFilepath, dst, imgio.JPEGEncoder(95)); err != nil {
		return nil, fmt.Errorf("error while saving image: %w", err)
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
