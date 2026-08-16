package transcode

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/storage"
)

func newWorkerTestEnv(t *testing.T) (*gorm.DB, *models.User, *storage.MemoryBackend, *config.Config) {
	t.Helper()

	dsn := fmt.Sprintf("file:worker-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.File{}, &models.TranscodeJob{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	user := &models.User{Username: "u", Email: "u@example.com", StorageQuota: 1 << 30}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	ffprobe := writeFakeProbe(t, `{"format":{"format_name":"matroska,webm"},"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080},{"codec_type":"audio","codec_name":"aac"}]}`)
	ffmpeg := writeFakeFFmpeg(t, "fake-transcoded-video-output")

	cfg := &config.Config{
		TempDir:               t.TempDir(),
		TranscodeTimeout:      time.Minute,
		TranscodeMaxAttempts:  3,
		TranscodePollInterval: time.Second,
		TranscodeMaxHeight:    720,
		TranscodePreset:       "ultrafast",
		TranscodeCRF:          28,
		FFmpegPath:            ffmpeg,
		FFprobePath:           ffprobe,
		TranscodeStaleJobAge:  30 * time.Minute,
	}

	return db, user, storage.NewMemoryBackend(), cfg
}

func addWorkerFile(t *testing.T, db *gorm.DB, user *models.User, backend storage.StorageBackend, filename, mime, content string) *models.File {
	t.Helper()
	result, err := backend.Save(context.Background(), strings.NewReader(content), storage.SaveOptions{
		OriginalFilename: filename,
		ContentType:      mime,
	})
	if err != nil {
		t.Fatalf("failed to save file to storage: %v", err)
	}
	file := &models.File{
		UserID:           user.ID,
		StoragePath:      result.Path,
		LogicalPath:      "/",
		Filename:         filename,
		OriginalFilename: filename,
		FileSize:         result.Size,
		MimeType:         mime,
		Hash:             result.Hash,
		UploadStatus:     "completed",
	}
	if err := db.Create(file).Error; err != nil {
		t.Fatalf("failed to create file record: %v", err)
	}
	if err := db.Model(user).UpdateColumn("storage_used", gorm.Expr("storage_used + ?", result.Size)).Error; err != nil {
		t.Fatalf("failed to update quota: %v", err)
	}
	return file
}

func reloadFile(t *testing.T, db *gorm.DB, id uint) models.File {
	t.Helper()
	var file models.File
	if err := db.First(&file, id).Error; err != nil {
		t.Fatalf("failed to reload file %d: %v", id, err)
	}
	return file
}

func reloadUser(t *testing.T, db *gorm.DB, id uint) models.User {
	t.Helper()
	var user models.User
	if err := db.First(&user, id).Error; err != nil {
		t.Fatalf("failed to reload user %d: %v", id, err)
	}
	return user
}

func TestWorkerTranscodesNonCompliantVideo(t *testing.T) {
	db, user, mem, cfg := newWorkerTestEnv(t)
	file := addWorkerFile(t, db, user, mem, "movie.mkv", "video/x-matroska", "fake-mkv-content")
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	worker := NewWorker(db, cfg, mem)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	updated := reloadFile(t, db, file.ID)
	if updated.TranscodeStatus != StatusCompleted {
		t.Errorf("file status = %q, want completed", updated.TranscodeStatus)
	}
	if updated.VideoVariantPath == "" || updated.VideoVariantPath == file.StoragePath {
		t.Errorf("expected a new variant path, got %q", updated.VideoVariantPath)
	}
	if updated.VideoVariantMime != "video/mp4" {
		t.Errorf("variant mime = %q", updated.VideoVariantMime)
	}
	if updated.VideoVariantSize != int64(len("fake-transcoded-video-output")) {
		t.Errorf("variant size = %d", updated.VideoVariantSize)
	}
	if !strings.HasSuffix(updated.VideoVariantPath, ".mp4") {
		t.Errorf("variant path should end in .mp4: %q", updated.VideoVariantPath)
	}

	// The variant counts toward quota.
	updatedUser := reloadUser(t, db, user.ID)
	wantUsed := file.FileSize + updated.VideoVariantSize
	if updatedUser.StorageUsed != wantUsed {
		t.Errorf("storage_used = %d, want %d", updatedUser.StorageUsed, wantUsed)
	}

	// Job row removed.
	var count int64
	db.Model(&models.TranscodeJob{}).Where("file_id = ?", file.ID).Count(&count)
	if count != 0 {
		t.Errorf("expected job row removed, found %d", count)
	}

	// Variant stored in backend.
	if _, err := mem.Stat(context.Background(), updated.VideoVariantPath); err != nil {
		t.Errorf("variant not in storage: %v", err)
	}
}

func TestWorkerRemuxesCompatibleContainer(t *testing.T) {
	db, user, mem, cfg := newWorkerTestEnv(t)
	// 1280x720 h264/aac mkv: compatible streams, non-MP4 container -> remux.
	ffprobe := writeFakeProbe(t, `{"format":{"format_name":"matroska,webm"},"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720},{"codec_type":"audio","codec_name":"aac"}]}`)
	cfg.FFprobePath = ffprobe

	file := addWorkerFile(t, db, user, mem, "clip.mkv", "video/x-matroska", "fake-mkv-content")
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	worker := NewWorker(db, cfg, mem)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	updated := reloadFile(t, db, file.ID)
	if updated.TranscodeStatus != StatusCompleted {
		t.Errorf("file status = %q, want completed", updated.TranscodeStatus)
	}
	if updated.VideoVariantPath == file.StoragePath {
		t.Error("expected remux to produce a new variant")
	}
}

func TestWorkerSkipsAlreadyOptimizedMP4(t *testing.T) {
	db, user, mem, cfg := newWorkerTestEnv(t)
	// Compliant 720p h264/aac MP4 with faststart.
	ffprobe := writeFakeProbe(t, `{"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2"},"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720},{"codec_type":"audio","codec_name":"aac"}]}`)
	cfg.FFprobePath = ffprobe

	mp4Content := buildMP4("ftypisom", "moov", "mdat")
	file := addWorkerFile(t, db, user, mem, "movie.mp4", "video/mp4", string(mp4Content))
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	worker := NewWorker(db, cfg, mem)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	updated := reloadFile(t, db, file.ID)
	if updated.TranscodeStatus != StatusCompleted {
		t.Errorf("file status = %q, want completed", updated.TranscodeStatus)
	}
	// The variant reuses the original object.
	if updated.VideoVariantPath != file.StoragePath {
		t.Errorf("variant path = %q, want original %q", updated.VideoVariantPath, file.StoragePath)
	}
	// No extra quota for a shared object.
	updatedUser := reloadUser(t, db, user.ID)
	if updatedUser.StorageUsed != file.FileSize {
		t.Errorf("storage_used = %d, want %d", updatedUser.StorageUsed, file.FileSize)
	}
}

func TestWorkerFailsPermanentlyAfterMaxAttempts(t *testing.T) {
	db, user, mem, cfg := newWorkerTestEnv(t)
	cfg.TranscodeMaxAttempts = 1
	cfg.FFprobePath = writeFakeScript(t, "ffprobe", "echo 'not a video' >&2; exit 1")

	file := addWorkerFile(t, db, user, mem, "broken.mkv", "video/x-matroska", "garbage")
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	worker := NewWorker(db, cfg, mem)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	updated := reloadFile(t, db, file.ID)
	if updated.TranscodeStatus != StatusFailed {
		t.Errorf("file status = %q, want failed", updated.TranscodeStatus)
	}
	if updated.TranscodeError == "" {
		t.Error("expected an error message")
	}

	var job models.TranscodeJob
	if err := db.Where("file_id = ?", file.ID).First(&job).Error; err != nil {
		t.Fatalf("failed job row should be kept: %v", err)
	}
	if job.Status != JobFailed {
		t.Errorf("job status = %q, want failed", job.Status)
	}

	// Original is untouched.
	if _, err := mem.Stat(context.Background(), updated.StoragePath); err != nil {
		t.Errorf("original missing from storage: %v", err)
	}
}

func TestWorkerRetriesTransientFailure(t *testing.T) {
	db, user, mem, cfg := newWorkerTestEnv(t)
	cfg.TranscodeMaxAttempts = 3
	cfg.FFprobePath = writeFakeScript(t, "ffprobe", "echo 'flaky' >&2; exit 1")

	file := addWorkerFile(t, db, user, mem, "flaky.mkv", "video/x-matroska", "content")
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	worker := NewWorker(db, cfg, mem)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// First attempt failed and the job is waiting for its backoff.
	var job models.TranscodeJob
	if err := db.Where("file_id = ?", file.ID).First(&job).Error; err != nil {
		t.Fatalf("job row missing: %v", err)
	}
	if job.Status != JobPending || job.Attempts != 1 {
		t.Errorf("job = %+v, want pending with 1 attempt", job)
	}
	if job.NextAttemptAt == nil || !job.NextAttemptAt.After(time.Now()) {
		t.Errorf("expected future next_attempt_at, got %v", job.NextAttemptAt)
	}

	// Manually advance the backoff and re-run to exhaustion.
	if err := db.Model(&models.TranscodeJob{}).Where("id = ?", job.ID).Update("next_attempt_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("failed to advance backoff: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := worker.Run(context.Background(), true); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := db.Model(&models.TranscodeJob{}).Where("id = ?", job.ID).Update("next_attempt_at", time.Now().Add(-time.Second)).Error; err != nil {
			t.Fatalf("failed to advance backoff: %v", err)
		}
	}

	updated := reloadFile(t, db, file.ID)
	if updated.TranscodeStatus != StatusFailed {
		t.Errorf("file status = %q, want failed after 3 attempts", updated.TranscodeStatus)
	}
}

func TestWorkerDiscardsOrphanedJob(t *testing.T) {
	db, user, mem, cfg := newWorkerTestEnv(t)
	file := addWorkerFile(t, db, user, mem, "movie.mkv", "video/x-matroska", "content")
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	// Simulate the file being permanently deleted while the job is queued.
	if err := db.Unscoped().Delete(&models.File{}, file.ID).Error; err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}

	worker := NewWorker(db, cfg, mem)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var count int64
	db.Model(&models.TranscodeJob{}).Where("file_id = ?", file.ID).Count(&count)
	if count != 0 {
		t.Errorf("orphaned job row should be discarded, found %d", count)
	}
}
