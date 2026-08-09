package media

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A fake R2 endpoint records the signed PUT: method, path, body and the
// SigV4 header structure.
func TestR2PutRequest(t *testing.T) {
	content := []byte("r2 object bytes")
	fixed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	var gotMethod, gotPath, gotAuth, gotPayloadHash, gotAmzDate, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		gotAuth = r.Header.Get("Authorization")
		gotPayloadHash = r.Header.Get("x-amz-content-sha256")
		gotAmzDate = r.Header.Get("x-amz-date")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &R2Client{
		accessKeyID:     "ak",
		secretAccessKey: "sk",
		bucket:          "media",
		endpoint:        srv.URL,
		httpClient:      srv.Client(),
		now:             func() time.Time { return fixed },
	}
	err := c.Put("a/b/c.jpg", content, "image/jpeg")
	assert.NoError(t, err)

	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "/media/a/b/c.jpg", gotPath)
	assert.Equal(t, content, gotBody)
	assert.Equal(t, sha256Hex(content), gotPayloadHash)
	assert.Equal(t, "20240102T030405Z", gotAmzDate)
	assert.Equal(t, "image/jpeg", gotContentType)

	authPattern := regexp.MustCompile(
		`^AWS4-HMAC-SHA256 Credential=ak/20240102/auto/s3/aws4_request, ` +
			`SignedHeaders=host;x-amz-content-sha256;x-amz-date, ` +
			`Signature=[0-9a-f]{64}$`)
	assert.Regexp(t, authPattern, gotAuth)
}

func TestR2PutNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()

	c := &R2Client{
		accessKeyID:     "ak",
		secretAccessKey: "sk",
		bucket:          "media",
		endpoint:        srv.URL,
		httpClient:      srv.Client(),
		now:             time.Now,
	}
	err := c.Put("a/b/c.jpg", []byte("x"), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// AWS test-vector style known answer: fixed keys, clock and request. The
// expected values were computed with an independent implementation of the
// SigV4 algorithm, so this pins the canonical request construction and the
// signature derivation, and guarantees reproducibility.
func TestSigV4SignatureKnownAnswer(t *testing.T) {
	content := []byte("hello r2")
	fixed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	payloadHash := "f18d11218e7814319b7a555383d8eaa42b97ae6ec36594f37f1e256b0a386b12"
	assert.Equal(t, sha256Hex(content), payloadHash)

	c := &R2Client{
		accessKeyID:     "test-access-key",
		secretAccessKey: "test-secret-key",
		bucket:          "media",
	}
	sign := func() *http.Request {
		req, err := http.NewRequest(http.MethodPut,
			"https://9f4b2c.r2.cloudflarestorage.com/media/a/b/c.jpg",
			bytes.NewReader(content))
		assert.NoError(t, err)
		c.sign(req, "/media/a/b/c.jpg", payloadHash, fixed)
		return req
	}

	req := sign()
	canonicalRequest, signedHeaders := canonicalSigV4Request(req, "/media/a/b/c.jpg", payloadHash)
	assert.Equal(t, "host;x-amz-content-sha256;x-amz-date", signedHeaders)
	assert.Equal(t,
		"PUT\n"+
			"/media/a/b/c.jpg\n"+
			"\n"+
			"host:9f4b2c.r2.cloudflarestorage.com\n"+
			"x-amz-content-sha256:"+payloadHash+"\n"+
			"x-amz-date:20240102T030405Z\n"+
			"\n"+
			"host;x-amz-content-sha256;x-amz-date\n"+
			payloadHash,
		canonicalRequest)

	wantAuth := "AWS4-HMAC-SHA256 " +
		"Credential=test-access-key/20240102/auto/s3/aws4_request, " +
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
		"Signature=05f3cc23f4ea36b0746a2d7eee840942c1fbdba94a29adc66f80cf8a5f3fe5ce"
	assert.Equal(t, wantAuth, req.Header.Get("Authorization"))

	// Signing is deterministic: the same inputs reproduce the signature.
	assert.Equal(t, wantAuth, sign().Header.Get("Authorization"))
}

func TestSigV4Escape(t *testing.T) {
	assert.Equal(t, "a/b/c.jpg", sigV4EscapeKey("a/b/c.jpg"))
	assert.Equal(t, "a%20b/%24x", sigV4EscapeKey("a b/$x"))
	assert.Equal(t, "-._~", sigV4Escape("-._~"))
}
