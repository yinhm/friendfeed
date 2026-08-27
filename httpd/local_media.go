package main

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/media"
)

var inlineMediaExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

// localMediaHandler preserves the legacy /file fallback without allowing
// uploaded active content to execute under the ffweb origin. Extensionless
// historical mirrors retain their old behavior; every staged non-raster and
// every new typed attachment is forced to download.
func localMediaHandler(root string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rel := strings.TrimPrefix(filepath.Clean("/"+c.Param("filepath")), string(filepath.Separator))
		if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		ext := strings.ToLower(filepath.Ext(rel))
		staged := strings.HasPrefix(filepath.ToSlash(rel), media.StagingDirectory+"/")
		if (staged && !inlineMediaExtensions[ext]) || isAttachmentExtension(ext) {
			c.Header("Content-Type", "application/octet-stream")
			c.Header("Content-Disposition", "attachment")
			c.Header("X-Content-Type-Options", "nosniff")
		} else if inlineMediaExtensions[ext] {
			c.Header("X-Content-Type-Options", "nosniff")
		}
		http.ServeFile(c.Writer, c.Request, filepath.Join(root, rel))
	}
}

func isAttachmentExtension(ext string) bool {
	switch ext {
	case ".svg", ".pdf", ".txt", ".md", ".markdown", ".csv", ".json", ".xml", ".html", ".htm", ".rtf",
		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".odt", ".ods", ".odp",
		".zip", ".7z", ".rar", ".tar", ".gz", ".gzip", ".bz2", ".xz",
		".mp3", ".m4a", ".aac", ".ogg", ".opus", ".flac", ".wav", ".wma",
		".mp4", ".m4v", ".webm", ".mov", ".ogv", ".mpeg", ".mpg", ".mkv", ".avi", ".wmv", ".3gp",
		".epub", ".mobi", ".azw":
		return true
	default:
		return false
	}
}
