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

func TestPromoteEntryImagesRewritesAndRemovesTemporaryState(t *testing.T) {
	cfg := &util.Config{MediaPath: t.TempDir(), MediaURL: "https://media.example"}
	staging := media.NewStagingStore(cfg)
	content := []byte("image bytes")
	name, digest, err := staging.Put(content, "jpg")
	require.NoError(t, err)
	payload := &assetTokenPayload{Version: 1, Actor: "actor", Kind: "image", Width: 10, Height: 20, Expires: time.Now().Add(time.Hour).Unix(), Objects: []stagedObject{{Name: name, Digest: digest, Extension: "jpg", MimeType: "image/jpeg", Size: len(content), Role: "original"}}}
	token := "signed-token"
	stagingURL := cfg.MediaURL + "/upload-staging/" + name
	raw := `[{"type":"img","url":"` + stagingURL + `","originalUrl":"` + stagingURL + `","assetToken":"` + token + `","children":[{"text":""}]}]`
	body := `<img src="` + stagingURL + `">`
	server := &Server{staging: staging, mediaBaseURL: cfg.MediaURL}
	raw, body, thumbnails, err := server.promoteEntryImages("", raw, body, nil, map[string]*assetTokenPayload{token: payload})
	require.NoError(t, err)
	require.NotContains(t, raw, "upload-staging")
	require.NotContains(t, raw, "assetToken")
	require.NotContains(t, body, "upload-staging")
	require.Len(t, thumbnails, 1)
	require.True(t, strings.HasSuffix(thumbnails[0].Url, ".jpg"))
}

func TestPromoteEntryImagesPreservesNonPlateSourceThumbnails(t *testing.T) {
	server := &Server{mediaBaseURL: "https://media.example"}
	old := []*pb.Thumbnail{{Url: "https://source.example/thumb.jpg", Link: "https://source.example/original.jpg"}}
	raw := `[{"type":"p","children":[{"text":"edited source entry"}]}]`
	_, _, thumbnails, err := server.promoteEntryImages(raw, raw, "<p>edited source entry</p>", old, nil)
	require.NoError(t, err)
	require.Equal(t, old, thumbnails)
}

func TestPromoteEntryImagesRejectsTokenWithoutStagingURLs(t *testing.T) {
	server := &Server{}
	raw := `[{"type":"img","assetToken":"token","children":[{"text":""}]}]`
	_, _, _, err := server.promoteEntryImages("", raw, "<p>safe</p>", nil, map[string]*assetTokenPayload{"token": {Kind: "image"}})
	require.ErrorContains(t, err, "missing its staging URLs")
}
