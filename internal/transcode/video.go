package transcode

import (
	"path/filepath"
	"strings"
)

// Status values used on models.File.TranscodeStatus.
const (
	StatusNone       = "none"
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

// Job status values used on models.TranscodeJob.Status.
const (
	JobPending    = "pending"
	JobProcessing = "processing"
	JobFailed     = "failed"
)

// videoExtensions is the set of filename extensions treated as videos,
// used as a fallback when the client-supplied MIME type is unreliable.
var videoExtensions = map[string]bool{
	".mkv": true, ".avi": true, ".mov": true, ".mp4": true, ".m4v": true,
	".webm": true, ".wmv": true, ".flv": true, ".ts": true, ".mts": true,
	".m2ts": true, ".3gp": true, ".3g2": true, ".ogv": true, ".mpg": true,
	".mpeg": true, ".vob": true, ".rm": true, ".rmvb": true, ".divx": true,
	".f4v": true,
}

// IsVideoFile reports whether a file should be considered a video based on
// its MIME type or filename extension.
func IsVideoFile(mimeType, filename string) bool {
	if strings.HasPrefix(strings.ToLower(mimeType), "video/") {
		return true
	}
	return videoExtensions[strings.ToLower(filepath.Ext(filename))]
}
