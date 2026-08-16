package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/storage"
	"github.com/agjmills/trove/internal/transcode"
)

// Stream serves the web-optimized video variant with HTTP Range support so
// HTML5 players can seek. The original file remains available via /download.
func (h *FileHandler) Stream(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "File ID is required", http.StatusBadRequest)
		return
	}

	var file models.File
	if err := h.db.Where("id = ? AND user_id = ?", fileID, user.ID).First(&file).Error; err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// The variant is only streamable once transcoding completed.
	if file.VideoVariantPath == "" || file.TranscodeStatus != transcode.StatusCompleted {
		http.Error(w, "Video variant is not ready yet", http.StatusNotFound)
		return
	}

	ctx := r.Context()

	info, err := h.storage.Stat(ctx, file.VideoVariantPath)
	if errors.Is(err, storage.ErrNotFound) {
		http.Error(w, "Video variant not found in storage", http.StatusNotFound)
		return
	}
	if err != nil {
		logger.Error("failed to stat video variant", "error", err, "path", file.VideoVariantPath)
		http.Error(w, "Failed to access video", http.StatusInternalServerError)
		return
	}
	size := info.Size

	contentType := file.VideoVariantMime
	if contentType == "" {
		contentType = "video/mp4"
	}

	start, end, ranged := parseRangeHeader(r.Header.Get("Range"), size)

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")

	if ranged {
		if start >= size {
			// Requested range is not satisfiable.
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		length := end - start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		reader, err := h.storage.OpenRange(ctx, file.VideoVariantPath, start, length)
		if err != nil {
			logger.Error("failed to open video range", "error", err, "path", file.VideoVariantPath)
			return
		}
		defer reader.Close() //nolint:errcheck
		if _, err := io.Copy(w, reader); err != nil {
			logger.Debug("error streaming video range", "error", err, "path", file.VideoVariantPath)
		}
		return
	}

	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	reader, err := h.storage.Open(ctx, file.VideoVariantPath)
	if err != nil {
		logger.Error("failed to open video", "error", err, "path", file.VideoVariantPath)
		return
	}
	defer reader.Close() //nolint:errcheck
	if _, err := io.Copy(w, reader); err != nil {
		logger.Debug("error streaming video", "error", err, "path", file.VideoVariantPath)
	}
}

// parseRangeHeader parses a single-range "Range" header of the form
// "bytes=start-end", "bytes=start-" or "bytes=-suffix".
// It returns the inclusive start/end offsets and false if no valid range was
// provided (multi-range or malformed headers fall back to a full response).
func parseRangeHeader(header string, size int64) (start, end int64, ok bool) {
	if header == "" || !strings.HasPrefix(header, "bytes=") || size <= 0 {
		return 0, 0, false
	}

	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false // multi-range requests are served as full content
	}

	end = size - 1

	if dash := strings.Index(spec, "-"); dash >= 0 {
		startStr, endStr := strings.TrimSpace(spec[:dash]), strings.TrimSpace(spec[dash+1:])

		switch {
		case startStr == "" && endStr == "":
			return 0, 0, false
		case startStr == "":
			// Suffix range: last N bytes.
			suffix, err := strconv.ParseInt(endStr, 10, 64)
			if err != nil || suffix <= 0 {
				return 0, 0, false
			}
			if suffix > size {
				suffix = size
			}
			start = size - suffix
		default:
			var err error
			start, err = strconv.ParseInt(startStr, 10, 64)
			if err != nil || start < 0 {
				return 0, 0, false
			}
			if endStr != "" {
				end, err = strconv.ParseInt(endStr, 10, 64)
				if err != nil || end < start {
					return 0, 0, false
				}
			}
			if end >= size {
				end = size - 1
			}
		}
	} else {
		return 0, 0, false
	}

	return start, end, true
}
