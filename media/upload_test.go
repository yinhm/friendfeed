package media

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"testing"

	"github.com/stretchr/testify/require"
)

func jpegBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, jpeg.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height)), nil))
	return out.Bytes()
}

func TestPrepareUploadedImageValidatesAndResizes(t *testing.T) {
	prepared, err := PrepareUploadedImage(jpegBytes(t, 140, 70), 50)
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", prepared.MimeType)
	require.Equal(t, 140, prepared.Width)
	require.Equal(t, 70, prepared.Height)
	require.Equal(t, 50, prepared.ThumbnailWidth)
	require.Equal(t, 25, prepared.ThumbnailHeight)
	_, format, err := image.Decode(bytes.NewReader(prepared.Thumbnail))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)

	_, err = PrepareUploadedImage([]byte("not an image"), 50)
	require.Error(t, err)
}

func TestPrepareUploadedImageRejectsPixelBomb(t *testing.T) {
	// GIF DecodeConfig only needs the logical-screen descriptor, so this
	// proves oversized dimensions are rejected before allocating pixels.
	header := append([]byte("GIF89a"), make([]byte, 7)...)
	binary.LittleEndian.PutUint16(header[6:8], 10_000)
	binary.LittleEndian.PutUint16(header[8:10], 10_000)
	_, err := PrepareUploadedImage(header, 50)
	require.ErrorContains(t, err, "invalid image dimensions")
}

func TestInspectAttachmentAllowlist(t *testing.T) {
	info, err := InspectAttachment("../report.HTML", []byte("<!doctype html><title>x</title>"))
	require.NoError(t, err)
	require.Equal(t, "report.HTML", info.Name)
	require.Equal(t, "text/html", info.MimeType)

	info, err = InspectAttachment("song.mp3", append([]byte("ID3"), make([]byte, 20)...))
	require.NoError(t, err)
	require.Equal(t, "audio/mpeg", info.MimeType)

	_, err = InspectAttachment("payload.exe", []byte("MZ executable"))
	require.Error(t, err)
	_, err = InspectAttachment("fake.pdf", []byte("plain text"))
	require.Error(t, err)
	_, err = InspectAttachment("script.js", []byte("alert(1)"))
	require.Error(t, err)
}

func TestInspectAttachmentRecognizesOfficeContainer(t *testing.T) {
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, name := range []string{"[Content_Types].xml", "word/document.xml"} {
		part, err := w.Create(name)
		require.NoError(t, err)
		_, err = part.Write([]byte("x"))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	info, err := InspectAttachment("report.docx", out.Bytes())
	require.NoError(t, err)
	require.Contains(t, info.MimeType, "wordprocessingml")
}
