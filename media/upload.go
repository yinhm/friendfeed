package media

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/anthonynsimon/bild/imgio"
	"github.com/anthonynsimon/bild/transform"
)

const (
	MaxUploadFileBytes = 20 << 20
	MaxImagePixels     = 40_000_000
)

type PreparedImage struct {
	Original        []byte
	Thumbnail       []byte
	MimeType        string
	Width           int
	Height          int
	ThumbnailWidth  int
	ThumbnailHeight int
}

// PrepareUploadedImage validates image bytes before anything is persisted and
// creates an in-memory JPEG thumbnail when the source is meaningfully wider
// than maxWidth. The original bytes are never re-encoded.
func PrepareUploadedImage(content []byte, maxWidth int) (*PreparedImage, error) {
	if len(content) == 0 {
		return nil, errors.New("image is empty")
	}
	if len(content) > MaxUploadFileBytes {
		return nil, fmt.Errorf("image exceeds %d byte limit", MaxUploadFileBytes)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("decode image metadata: %w", err)
	}
	mimeType, ok := map[string]string{
		"jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif", "webp": "image/webp",
	}[format]
	if !ok {
		return nil, fmt.Errorf("unsupported image format %q", format)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return nil, fmt.Errorf("invalid image dimensions %dx%d", config.Width, config.Height)
	}
	prepared := &PreparedImage{
		Original: content, MimeType: mimeType, Width: config.Width, Height: config.Height,
		ThumbnailWidth: config.Width, ThumbnailHeight: config.Height,
	}
	if maxWidth <= 0 || config.Width <= int(float64(maxWidth)*1.3) {
		prepared.Thumbnail = content
		return prepared, nil
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	height := int(float64(config.Height) * float64(maxWidth) / float64(config.Width))
	if height < 1 {
		height = 1
	}
	thumb := transform.Resize(source, maxWidth, height, transform.Lanczos)
	var encoded bytes.Buffer
	if err := imgio.JPEGEncoder(95)(&encoded, thumb); err != nil {
		return nil, fmt.Errorf("encode image thumbnail: %w", err)
	}
	prepared.Thumbnail = encoded.Bytes()
	prepared.ThumbnailWidth = maxWidth
	prepared.ThumbnailHeight = height
	return prepared, nil
}

type AttachmentInfo struct {
	Name     string
	MimeType string
	Size     int
}

// InspectAttachment applies the product allowlist from docs/media_upload.md.
// Extensions select a candidate format, but the bytes must independently
// match that format; multipart Content-Type is deliberately ignored.
func InspectAttachment(name string, content []byte) (*AttachmentInfo, error) {
	name = sanitizeDisplayFilename(name)
	if name == "" || len(content) == 0 {
		return nil, errors.New("attachment name and content are required")
	}
	if len(content) > MaxUploadFileBytes {
		return nil, fmt.Errorf("attachment exceeds %d byte limit", MaxUploadFileBytes)
	}
	ext := strings.ToLower(filepath.Ext(name))
	mimeType, ok := inspectAttachmentType(ext, content)
	if !ok {
		return nil, fmt.Errorf("unsupported attachment type %q", ext)
	}
	return &AttachmentInfo{Name: name, MimeType: mimeType, Size: len(content)}, nil
}

func sanitizeDisplayFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	for len(name) > 255 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func inspectAttachmentType(ext string, content []byte) (string, bool) {
	if mimeType, ok := inspectRasterImage(ext, content); ok {
		return mimeType, true
	}
	if ext == ".svg" && looksLikeText(content) && bytes.Contains(bytes.ToLower(content), []byte("<svg")) {
		return "image/svg+xml", true
	}
	if ext == ".pdf" && bytes.HasPrefix(content, []byte("%PDF-")) {
		return "application/pdf", true
	}
	if mimeType, ok := inspectTextDocument(ext, content); ok {
		return mimeType, true
	}
	if mimeType, ok := inspectOfficeOrEbook(ext, content); ok {
		return mimeType, true
	}
	if mimeType, ok := inspectArchive(ext, content); ok {
		return mimeType, true
	}
	if mimeType, ok := inspectAudioVideo(ext, content); ok {
		return mimeType, true
	}
	if (ext == ".mobi" || ext == ".azw") && len(content) >= 68 && string(content[60:68]) == "BOOKMOBI" {
		return "application/x-mobipocket-ebook", true
	}
	return "", false
}

func inspectRasterImage(ext string, content []byte) (string, bool) {
	allowed := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp"}
	want, ok := allowed[ext]
	if !ok {
		return "", false
	}
	prepared, err := PrepareUploadedImage(content, 0)
	return want, err == nil && prepared.MimeType == want
}

func inspectTextDocument(ext string, content []byte) (string, bool) {
	types := map[string]string{
		".txt": "text/plain", ".md": "text/markdown", ".markdown": "text/markdown",
		".csv": "text/csv", ".json": "application/json", ".xml": "application/xml",
		".html": "text/html", ".htm": "text/html", ".rtf": "application/rtf",
	}
	mimeType, ok := types[ext]
	if !ok || !looksLikeText(content) {
		return "", false
	}
	if ext == ".json" && !json.Valid(content) {
		return "", false
	}
	if ext == ".rtf" && !bytes.HasPrefix(bytes.TrimSpace(content), []byte(`{\rtf`)) {
		return "", false
	}
	return mimeType, true
}

func looksLikeText(content []byte) bool {
	return utf8.Valid(content) && !bytes.ContainsRune(content, '\x00')
}

func inspectOfficeOrEbook(ext string, content []byte) (string, bool) {
	legacy := map[string]string{".doc": "application/msword", ".xls": "application/vnd.ms-excel", ".ppt": "application/vnd.ms-powerpoint"}
	if mimeType, ok := legacy[ext]; ok {
		return mimeType, len(content) >= 8 && bytes.Equal(content[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1})
	}
	if !isZip(content) {
		return "", false
	}
	entries, mimeMarker, err := zipMetadata(content)
	if err != nil {
		return "", false
	}
	prefixes := map[string]struct{ prefix, mime string }{
		".docx": {"word/", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		".xlsx": {"xl/", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		".pptx": {"ppt/", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	}
	if spec, ok := prefixes[ext]; ok {
		return spec.mime, entries["[Content_Types].xml"] && hasEntryPrefix(entries, spec.prefix)
	}
	openDocument := map[string]string{
		".odt": "application/vnd.oasis.opendocument.text", ".ods": "application/vnd.oasis.opendocument.spreadsheet",
		".odp": "application/vnd.oasis.opendocument.presentation", ".epub": "application/epub+zip",
	}
	if mimeType, ok := openDocument[ext]; ok {
		return mimeType, entries["mimetype"] && mimeMarker == mimeType
	}
	return "", false
}

func zipMetadata(content []byte) (map[string]bool, string, error) {
	r, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, "", err
	}
	entries := make(map[string]bool, len(r.File))
	mimeMarker := ""
	for _, file := range r.File {
		entries[file.Name] = true
		if file.Name == "mimetype" && file.UncompressedSize64 <= 256 {
			reader, openErr := file.Open()
			if openErr != nil {
				return nil, "", openErr
			}
			var marker bytes.Buffer
			_, copyErr := marker.ReadFrom(reader)
			closeErr := reader.Close()
			if copyErr != nil {
				return nil, "", copyErr
			}
			if closeErr != nil {
				return nil, "", closeErr
			}
			mimeMarker = marker.String()
		}
	}
	return entries, mimeMarker, nil
}

func hasEntryPrefix(entries map[string]bool, prefix string) bool {
	for name := range entries {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func inspectArchive(ext string, content []byte) (string, bool) {
	switch ext {
	case ".zip":
		return "application/zip", isZip(content)
	case ".7z":
		return "application/x-7z-compressed", bytes.HasPrefix(content, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c})
	case ".rar":
		return "application/vnd.rar", bytes.HasPrefix(content, []byte("Rar!\x1a\x07"))
	case ".tar":
		return "application/x-tar", len(content) > 262 && string(content[257:262]) == "ustar"
	case ".gz", ".gzip":
		return "application/gzip", bytes.HasPrefix(content, []byte{0x1f, 0x8b})
	case ".bz2":
		return "application/x-bzip2", bytes.HasPrefix(content, []byte("BZh"))
	case ".xz":
		return "application/x-xz", bytes.HasPrefix(content, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00})
	}
	return "", false
}

func isZip(content []byte) bool {
	return bytes.HasPrefix(content, []byte{'P', 'K', 0x03, 0x04}) || bytes.HasPrefix(content, []byte{'P', 'K', 0x05, 0x06})
}

func inspectAudioVideo(ext string, content []byte) (string, bool) {
	switch ext {
	case ".mp3":
		return "audio/mpeg", bytes.HasPrefix(content, []byte("ID3")) || len(content) > 1 && content[0] == 0xff && content[1]&0xe0 == 0xe0
	case ".flac":
		return "audio/flac", bytes.HasPrefix(content, []byte("fLaC"))
	case ".wav":
		return "audio/wav", riffType(content, "WAVE")
	case ".wma", ".wmv":
		return map[bool]string{true: "audio/x-ms-wma", false: "video/x-ms-wmv"}[ext == ".wma"], bytes.HasPrefix(content, []byte{0x30, 0x26, 0xb2, 0x75, 0x8e, 0x66, 0xcf, 0x11})
	case ".ogg", ".opus", ".ogv":
		mimeType := "audio/ogg"
		if ext == ".ogv" {
			mimeType = "video/ogg"
		}
		return mimeType, bytes.HasPrefix(content, []byte("OggS"))
	case ".aac":
		return "audio/aac", len(content) > 1 && content[0] == 0xff && content[1]&0xf6 == 0xf0
	case ".m4a", ".mp4", ".m4v", ".mov", ".3gp":
		mimeType := map[string]string{".m4a": "audio/mp4", ".mp4": "video/mp4", ".m4v": "video/x-m4v", ".mov": "video/quicktime", ".3gp": "video/3gpp"}[ext]
		return mimeType, len(content) >= 12 && string(content[4:8]) == "ftyp"
	case ".webm", ".mkv":
		mimeType := "video/webm"
		if ext == ".mkv" {
			mimeType = "video/x-matroska"
		}
		return mimeType, bytes.HasPrefix(content, []byte{0x1a, 0x45, 0xdf, 0xa3})
	case ".avi":
		return "video/x-msvideo", riffType(content, "AVI ")
	case ".mpeg", ".mpg":
		return "video/mpeg", bytes.HasPrefix(content, []byte{0x00, 0x00, 0x01, 0xba})
	}
	return "", false
}

func riffType(content []byte, kind string) bool {
	return len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == kind
}
