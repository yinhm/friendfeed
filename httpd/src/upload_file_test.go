package server

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
)

func TestAssetTokenIsBoundToActorExpiryAndSignature(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	payload := assetTokenPayload{Version: 1, Actor: "actor", Kind: "file", Name: "report.pdf", Expires: now.Add(time.Hour).Unix(), Objects: []stagedObject{{Name: "upload.pdf", Digest: strings.Repeat("a", 64), Extension: "pdf", MimeType: "application/pdf", Size: 10, Role: "file"}}}
	token, err := signAssetToken("secret", payload)
	require.NoError(t, err)
	got, err := verifyAssetToken("secret", token, "actor", now)
	require.NoError(t, err)
	require.Equal(t, payload.Name, got.Name)
	_, err = verifyAssetToken("secret", token, "other", now)
	require.Error(t, err)
	_, err = verifyAssetToken("secret", token, "actor", now.Add(2*time.Hour))
	require.Error(t, err)
	_, err = verifyAssetToken("secret", token+"x", "actor", now)
	require.Error(t, err)
}

func TestFilesForEntryPostPreservesExistingAndPromotesStaging(t *testing.T) {
	now := time.Now().UTC()
	cfg := &util.Config{MediaPath: t.TempDir(), MediaURL: "https://media.example"}
	staging := media.NewStagingStore(cfg)
	content := []byte("%PDF-1.7\nfile")
	name, digest, err := staging.Put(content, "pdf")
	require.NoError(t, err)
	server := &Server{secretKey: "secret", uploads: media.NewUploadPipeline(cfg)}
	entry := &pb.Entry{Files: []*pb.File{{Url: "https://legacy.example/keep.pdf", Name: "keep.pdf", Size: 3}, {Url: "https://legacy.example/remove.pdf", Name: "remove.pdf", Size: 4}}}
	payload := assetTokenPayload{Version: 1, Actor: "actor", Kind: "file", Name: "new.pdf", Expires: now.Add(time.Hour).Unix(), Objects: []stagedObject{{Name: name, Digest: digest, Extension: "pdf", MimeType: "application/pdf", Size: len(content), Role: "file"}}}
	token, err := signAssetToken("secret", payload)
	require.NoError(t, err)
	files, err := server.filesForEntryPost("actor", entry, true, []string{"https://legacy.example/keep.pdf"}, []string{token, token}, now)
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "keep.pdf", files[0].Name)
	require.Contains(t, files[1].Url, "https://media.example/")
	require.Contains(t, files[1].Url, ".pdf?download=")
}
