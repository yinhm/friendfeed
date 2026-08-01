package media

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yinhm/friendfeed/util"
)

// r2Region is the only region Cloudflare R2 accepts for SigV4 signing.
const r2Region = "auto"

// R2Client is a minimal S3-compatible client for a Cloudflare R2 bucket.
// It implements single-object PUT with AWS Signature Version 4 using only
// the standard library: the payload is in memory, so it is signed directly
// with its sha256 (no streaming/chunked signing).
type R2Client struct {
	accessKeyID     string
	secretAccessKey string
	bucket          string
	// endpoint is the bucket API origin, normally
	// https://<account_id>.r2.cloudflarestorage.com; tests point it at an
	// httptest server.
	endpoint   string
	httpClient *http.Client
	// now is the signing clock, replaceable for deterministic tests.
	now func() time.Time
}

func newR2Client(cfg *util.Config) *R2Client {
	return &R2Client{
		accessKeyID:     cfg.R2AccessKeyID,
		secretAccessKey: cfg.R2SecretAccessKey,
		bucket:          cfg.R2Bucket,
		endpoint:        "https://" + cfg.R2AccountID + ".r2.cloudflarestorage.com",
		httpClient:      &http.Client{Timeout: fetchTimeout},
		now:             time.Now,
	}
}

// r2Configured reports whether the config carries full R2 credentials.
func r2Configured(cfg *util.Config) bool {
	return cfg.R2AccountID != "" && cfg.R2AccessKeyID != "" &&
		cfg.R2SecretAccessKey != "" && cfg.R2Bucket != ""
}

// Put uploads content under key with a SigV4-signed S3 PUT request.
func (c *R2Client) Put(key string, content []byte, contentType string) error {
	payloadHash := sha256Hex(content)
	canonicalURI := "/" + sigV4Escape(c.bucket) + "/" + sigV4EscapeKey(key)
	req, err := http.NewRequest(http.MethodPut, c.endpoint+canonicalURI,
		bytes.NewReader(content))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.sign(req, canonicalURI, payloadHash, c.now().UTC())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("r2 put %s: unexpected status %s", key, resp.Status)
	}
	return nil
}

// sign sets the SigV4 headers (x-amz-content-sha256, x-amz-date and
// Authorization) for a single in-memory payload.
func (c *R2Client) sign(req *http.Request, canonicalURI, payloadHash string, now time.Time) {
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	scope := dateStamp + "/" + r2Region + "/s3/aws4_request"

	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	canonicalRequest, signedHeaders := canonicalSigV4Request(req, canonicalURI, payloadHash)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(c.signingKey(dateStamp), stringToSign))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+
		c.accessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+
		", Signature="+signature)
}

// canonicalSigV4Request builds the canonical request and returns it with
// the signed header list. Only host, x-amz-content-sha256 and x-amz-date
// are signed; Content-Type is informational.
func canonicalSigV4Request(req *http.Request, canonicalURI, payloadHash string) (string, string) {
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + req.Header.Get("x-amz-date") + "\n"
	return strings.Join([]string{
		req.Method,
		canonicalURI,
		req.URL.Query().Encode(), // empty: plain PUT carries no query
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n"), signedHeaders
}

// signingKey derives the SigV4 signing key for service s3 in the R2
// "auto" region.
func (c *R2Client) signingKey(dateStamp string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+c.secretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, r2Region)
	kService := hmacSHA256(kRegion, "s3")
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sigV4EscapeKey escapes each path segment of an object key per SigV4 URI
// encoding rules; the "/" separators are preserved.
func sigV4EscapeKey(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = sigV4Escape(segment)
	}
	return strings.Join(segments, "/")
}

// sigV4Escape encodes every byte outside the SigV4 unreserved set.
func sigV4Escape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
