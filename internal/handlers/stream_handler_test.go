package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/storage"
)

func TestParseRangeHeader(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		size       int64
		wantStart  int64
		wantEnd    int64
		wantRanged bool
	}{
		{"empty header", "", 100, 0, 0, false},
		{"basic range", "bytes=0-499", 1000, 0, 499, true},
		{"open ended", "bytes=500-", 1000, 500, 999, true},
		{"suffix range", "bytes=-500", 1000, 500, 999, true},
		{"suffix larger than file", "bytes=-2000", 1000, 0, 999, true},
		{"end past EOF clamped", "bytes=900-5000", 1000, 900, 999, true},
		{"multi range not supported", "bytes=0-0,2-2", 1000, 0, 0, false},
		{"malformed", "bytes=abc", 1000, 0, 0, false},
		{"wrong unit", "items=0-10", 1000, 0, 0, false},
		{"end before start", "bytes=50-10", 1000, 0, 0, false},
		{"negative start", "bytes=-5-10", 1000, 0, 0, false},
		{"start beyond size", "bytes=2000-3000", 1000, 2000, 999, true},
		{"single byte", "bytes=5-5", 1000, 5, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, ranged := parseRangeHeader(tt.header, tt.size)
			if ranged != tt.wantRanged || start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("parseRangeHeader(%q, %d) = (%d, %d, %v), want (%d, %d, %v)",
					tt.header, tt.size, start, end, ranged, tt.wantStart, tt.wantEnd, tt.wantRanged)
			}
		})
	}
}

// streamTestUsers generates unique usernames because handler tests share one
// in-memory SQLite database per process.
var streamTestUsers int

// setupStreamTest creates a user with a video file whose variant is ready.
func setupStreamTest(t *testing.T, app *fileTestApp) (*models.User, *models.File, []byte) {
	t.Helper()
	streamTestUsers++
	user := app.createTestUser(t, fmt.Sprintf("streamuser%d", streamTestUsers))

	ctx := context.Background()
	variantContent := []byte("fake-mp4-variant-content")
	result, err := app.storage.Save(ctx, strings.NewReader(string(variantContent)), storage.SaveOptions{
		OriginalFilename: "movie.mp4",
		ContentType:      "video/mp4",
	})
	if err != nil {
		t.Fatalf("failed to save variant: %v", err)
	}

	file := app.createTestFile(t, user, "movie.mkv", "original-video")
	if err := app.db.Model(file).Updates(map[string]interface{}{
		"mime_type":          "video/x-matroska",
		"video_variant_path": result.Path,
		"video_variant_size": result.Size,
		"video_variant_mime": "video/mp4",
		"transcode_status":   "completed",
	}).Error; err != nil {
		t.Fatalf("failed to update file record: %v", err)
	}
	return user, file, variantContent
}

func TestStreamServesFullVariant(t *testing.T) {
	app := newFileTestApp(t)
	app.router.Get("/stream/{id}", app.fileHandler.Stream)
	user, file, variant := setupStreamTest(t, app)

	req := app.authenticatedRequest(t, http.MethodGet, "/stream/"+streamTestID(file.ID), nil, user)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("missing Accept-Ranges header")
	}
	if w.Header().Get("Content-Type") != "video/mp4" {
		t.Errorf("content type = %q", w.Header().Get("Content-Type"))
	}
	if w.Body.String() != string(variant) {
		t.Errorf("body = %q, want %q", w.Body.Bytes(), variant)
	}
}

func TestStreamServesRangeRequest(t *testing.T) {
	app := newFileTestApp(t)
	app.router.Get("/stream/{id}", app.fileHandler.Stream)
	user, file, variant := setupStreamTest(t, app)

	req := app.authenticatedRequest(t, http.MethodGet, "/stream/"+streamTestID(file.ID), nil, user)
	req.Header.Set("Range", "bytes=5-9")
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if w.Header().Get("Content-Length") != "5" {
		t.Errorf("content length = %q", w.Header().Get("Content-Length"))
	}
	wantRange := "bytes 5-9/" + itoa(len(variant))
	if w.Header().Get("Content-Range") != wantRange {
		t.Errorf("content range = %q, want %q", w.Header().Get("Content-Range"), wantRange)
	}
	if w.Body.String() != string(variant)[5:10] {
		t.Errorf("body = %q", w.Body.Bytes())
	}
}

func TestStreamServesSuffixRange(t *testing.T) {
	app := newFileTestApp(t)
	app.router.Get("/stream/{id}", app.fileHandler.Stream)
	user, file, variant := setupStreamTest(t, app)

	req := app.authenticatedRequest(t, http.MethodGet, "/stream/"+streamTestID(file.ID), nil, user)
	req.Header.Set("Range", "bytes=-7")
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if w.Body.String() != string(variant)[len(variant)-7:] {
		t.Errorf("body = %q", w.Body.Bytes())
	}
}

func TestStreamUnsatisfiableRange(t *testing.T) {
	app := newFileTestApp(t)
	app.router.Get("/stream/{id}", app.fileHandler.Stream)
	user, file, variant := setupStreamTest(t, app)

	req := app.authenticatedRequest(t, http.MethodGet, "/stream/"+streamTestID(file.ID), nil, user)
	req.Header.Set("Range", "bytes=1000-2000")
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", w.Code)
	}
	if w.Header().Get("Content-Range") != "bytes */"+itoa(len(variant)) {
		t.Errorf("content range = %q", w.Header().Get("Content-Range"))
	}
}

func TestStreamVariantNotReady(t *testing.T) {
	app := newFileTestApp(t)
	app.router.Get("/stream/{id}", app.fileHandler.Stream)
	streamTestUsers++
	user := app.createTestUser(t, fmt.Sprintf("streamuser%d", streamTestUsers))
	file := app.createTestFile(t, user, "movie.mkv", "original-video")
	// No variant fields: transcode pending.
	if err := app.db.Model(file).Updates(map[string]interface{}{
		"mime_type":        "video/x-matroska",
		"transcode_status": "pending",
	}).Error; err != nil {
		t.Fatalf("failed to update file: %v", err)
	}

	req := app.authenticatedRequest(t, http.MethodGet, "/stream/"+streamTestID(file.ID), nil, user)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestStreamRequiresAuth(t *testing.T) {
	app := newFileTestApp(t)
	app.router.Get("/stream/{id}", app.fileHandler.Stream)
	_, file, _ := setupStreamTest(t, app)

	req := httptest.NewRequest(http.MethodGet, "/stream/"+streamTestID(file.ID), nil)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// streamTestID converts a file ID to a string for URL building.
func streamTestID(id uint) string {
	return itoa(int(id))
}
