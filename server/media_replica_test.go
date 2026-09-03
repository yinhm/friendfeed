package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
)

func TestCollectCanonicalMediaRefsUsesOnlyConfiguredMediaOrigin(t *testing.T) {
	key := "a/b/" + strings.Repeat("c", 62) + ".jpg"
	entry := &pb.Entry{
		Thumbnails: []*pb.Thumbnail{{Url: "https://media.example/" + key, Link: "https://foreign.example/" + key}},
		Files:      []*pb.File{{Url: "https://media.example/" + key + "?download=x", Type: "application/pdf"}},
	}
	refs := collectCanonicalMediaRefs(entry, "https://media.example")
	require.Len(t, refs, 1)
	require.Equal(t, "application/pdf", refs[key])
}

func TestCanonicalImageMimeSupportsAvatarFormats(t *testing.T) {
	require.Equal(t, "image/jpeg", canonicalImageMime("a/b/avatar.jpg"))
	require.Equal(t, "image/png", canonicalImageMime("a/b/avatar.png"))
}
