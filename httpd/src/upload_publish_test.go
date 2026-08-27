package server

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/media"
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
	raw, body, thumbnails, err := server.promoteEntryImages(raw, body, nil, map[string]*assetTokenPayload{token: payload})
	require.NoError(t, err)
	require.NotContains(t, raw, "upload-staging")
	require.NotContains(t, raw, "assetToken")
	require.NotContains(t, body, "upload-staging")
	require.Len(t, thumbnails, 1)
	require.True(t, strings.HasSuffix(thumbnails[0].Url, ".jpg"))
}
