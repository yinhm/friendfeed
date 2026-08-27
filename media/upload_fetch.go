package media

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

var ErrUploadFetchTimeout = errors.New("remote image timeout")

// FetchUploadedImage is the stricter browser-upload adapter around the
// archive fetch transport. It keeps the SSRF/redirect protections but applies
// the upload-specific 20 MiB body limit and returns no URL-bearing errors.
func FetchUploadedImage(rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid remote image URL")
	}
	req, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, errors.New("invalid remote image request")
	}
	resp, err := newFetchClient().Do(req)
	if err != nil {
		var netError interface{ Timeout() bool }
		if errors.As(err, &netError) && netError.Timeout() {
			return nil, ErrUploadFetchTimeout
		}
		return nil, fmt.Errorf("remote image fetch failed: %w", sanitizeFetchError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("remote image returned status %d", resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, MaxUploadFileBytes+1))
	if err != nil {
		return nil, errors.New("remote image body failed")
	}
	if len(content) > MaxUploadFileBytes {
		return nil, errors.New("remote image exceeds upload limit")
	}
	return content, nil
}

func sanitizeFetchError(_ error) error {
	return errors.New("network error")
}
