package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

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

const (
	// defaultMediaBaseURL is the front domain of the R2 media bucket;
	// mirrored objects are served from <mediaBaseURL>/<sharded path>.
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
	ls := &LocalStorage{
		path:         cfg.MediaPath,
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
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

func (c *LocalStorage) shardFilepath(filename string) (string, string) {
	if len(filename) <= 3 ||
		filepath.Dir(filename) != "." {
		return filename, filepath.Join(c.path, filename)
	}

	outFile := filepath.Join(filename[:1], filename[1:2], filename[2:])
	return outFile, filepath.Join(c.path, outFile)
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

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return obj, err
	}
	if err := os.WriteFile(fullPath, obj.Content, 0644); err != nil {
		return obj, err
	}

	obj.Path = outFile
	return obj, nil
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
	fromImage, err := imaging.Open(fullpath)
	if err != nil {
		return nil, fmt.Errorf("error while open image: %w", err)
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
