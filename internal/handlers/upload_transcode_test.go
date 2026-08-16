package handlers

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/transcode"
)

func uploadWithMime(t *testing.T, app *fileTestApp, user *models.User, filename, content, mime string) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("folder", "/"); err != nil {
		t.Fatalf("failed to write folder field: %v", err)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", mime)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	req := app.authenticatedRequest(t, http.MethodPost, "/upload", &buf, user)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)
	return w
}

func TestUploadEnqueuesTranscodeJob(t *testing.T) {
	app := newFileTestApp(t)
	app.cfg.TranscodeEnabled = true
	user := app.createTestUser(t, "transcode-uploader")

	w := uploadWithMime(t, app, user, "movie.mkv", "video-bytes", "video/x-matroska")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want 303", w.Code)
	}
	app.fileHandler.WaitForPendingUploads()

	var file models.File
	if err := app.db.Where("user_id = ?", user.ID).First(&file).Error; err != nil {
		t.Fatalf("file record not found: %v", err)
	}
	if file.TranscodeStatus != transcode.StatusPending {
		t.Errorf("file transcode status = %q, want pending", file.TranscodeStatus)
	}

	var job models.TranscodeJob
	if err := app.db.Where("file_id = ?", file.ID).First(&job).Error; err != nil {
		t.Fatalf("transcode job not found: %v", err)
	}
	if job.UserID != user.ID || job.Status != transcode.JobPending {
		t.Errorf("job = %+v", job)
	}
}

func TestUploadNonVideoSkipsTranscode(t *testing.T) {
	app := newFileTestApp(t)
	app.cfg.TranscodeEnabled = true
	user := app.createTestUser(t, "transcode-text-uploader")

	w := uploadWithMime(t, app, user, "notes.txt", "hello", "text/plain")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want 303", w.Code)
	}
	app.fileHandler.WaitForPendingUploads()

	var file models.File
	if err := app.db.Where("user_id = ?", user.ID).First(&file).Error; err != nil {
		t.Fatalf("file record not found: %v", err)
	}
	if file.TranscodeStatus != transcode.StatusNone {
		t.Errorf("file transcode status = %q, want none", file.TranscodeStatus)
	}

	var count int64
	app.db.Model(&models.TranscodeJob{}).Where("file_id = ?", file.ID).Count(&count)
	if count != 0 {
		t.Errorf("expected no transcode job, found %d", count)
	}
}

func TestUploadDeduplicationCopiesVariant(t *testing.T) {
	app := newFileTestApp(t)
	app.cfg.TranscodeEnabled = true
	user := app.createTestUser(t, "transcode-dedup")

	content := "identical-video-bytes"
	if w := uploadWithMime(t, app, user, "clip.mp4", content, "video/mp4"); w.Code != http.StatusSeeOther {
		t.Fatalf("first upload status = %d", w.Code)
	}
	app.fileHandler.WaitForPendingUploads()

	// Mark the first file as having a completed variant.
	var first models.File
	if err := app.db.Where("user_id = ?", user.ID).First(&first).Error; err != nil {
		t.Fatalf("file record not found: %v", err)
	}
	if err := app.db.Model(&first).Updates(map[string]interface{}{
		"video_variant_path": "shared-variant.mp4",
		"video_variant_size": 999,
		"video_variant_mime": "video/mp4",
		"transcode_status":   transcode.StatusCompleted,
	}).Error; err != nil {
		t.Fatalf("failed to set variant: %v", err)
	}

	// Upload the same content again: deduplicated, variant copied.
	if w := uploadWithMime(t, app, user, "clip.mp4", content, "video/mp4"); w.Code != http.StatusSeeOther {
		t.Fatalf("second upload status = %d", w.Code)
	}
	app.fileHandler.WaitForPendingUploads()

	var second models.File
	if err := app.db.Where("user_id = ? AND id != ?", user.ID, first.ID).First(&second).Error; err != nil {
		t.Fatalf("second file record not found: %v", err)
	}
	if second.VideoVariantPath != "shared-variant.mp4" {
		t.Errorf("variant path = %q, want copied value", second.VideoVariantPath)
	}
	if second.TranscodeStatus != transcode.StatusCompleted {
		t.Errorf("transcode status = %q, want completed", second.TranscodeStatus)
	}

	var count int64
	app.db.Model(&models.TranscodeJob{}).Where("file_id = ?", second.ID).Count(&count)
	if count != 0 {
		t.Errorf("deduplicated file should not get a job, found %d", count)
	}
}

func TestUploadDisabledTranscodeSkipsJob(t *testing.T) {
	app := newFileTestApp(t)
	app.cfg.TranscodeEnabled = false
	user := app.createTestUser(t, "transcode-disabled")

	if w := uploadWithMime(t, app, user, "movie.mkv", "video-bytes", "video/x-matroska"); w.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d", w.Code)
	}
	app.fileHandler.WaitForPendingUploads()

	var file models.File
	if err := app.db.Where("user_id = ?", user.ID).First(&file).Error; err != nil {
		t.Fatalf("file record not found: %v", err)
	}
	if file.TranscodeStatus != transcode.StatusNone {
		t.Errorf("file transcode status = %q, want none", file.TranscodeStatus)
	}
	var count int64
	app.db.Model(&models.TranscodeJob{}).Where("file_id = ?", file.ID).Count(&count)
	if count != 0 {
		t.Errorf("expected no transcode job, found %d", count)
	}
}
