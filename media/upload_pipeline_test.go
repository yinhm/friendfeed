package media

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/util"
)

func TestUploadPipelineStagesAndReusesCanonicalImage(t *testing.T) {
	pipeline := NewUploadPipeline(&util.Config{MediaPath: t.TempDir(), MediaURL: "https://media.example"})
	staged, err := pipeline.StageImage(jpegBytes(t, 140, 70), 50)
	require.NoError(t, err)
	require.Len(t, staged.Objects, 2)
	first, err := pipeline.PromoteImage(staged)
	require.NoError(t, err)
	second, err := pipeline.PromoteImage(staged)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.True(t, strings.HasPrefix(first.URL, "https://media.example/"))
	require.NotEqual(t, first.URL, first.ThumbnailURL)
}

func TestUploadPipelineRejectsSpoofAndChangedPromotionMetadata(t *testing.T) {
	pipeline := NewUploadPipeline(&util.Config{MediaPath: t.TempDir(), MediaURL: "https://media.example"})
	_, err := pipeline.StageAttachment("fake.pdf", []byte("not a PDF"))
	require.Error(t, err)

	staged, err := pipeline.StageAttachment("report.pdf", []byte("%PDF-1.7\nreport"))
	require.NoError(t, err)
	staged.Object.Digest = strings.Repeat("0", 64)
	_, err = pipeline.PromoteAttachment(staged)
	require.ErrorContains(t, err, "digest changed")
}

func TestUploadPipelinePreservesActiveContentAsAttachmentURL(t *testing.T) {
	pipeline := NewUploadPipeline(&util.Config{MediaPath: t.TempDir(), MediaURL: "https://media.example"})
	staged, err := pipeline.StageAttachment("page.html", []byte("<!doctype html><title>safe download</title>"))
	require.NoError(t, err)
	published, err := pipeline.PromoteAttachment(staged)
	require.NoError(t, err)
	require.Equal(t, "text/html", published.MimeType)
	require.Contains(t, published.URL, "?download=")
}
