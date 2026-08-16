package transcode

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/agjmills/trove/internal/database/models"
)

func newJobsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:jobs-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.File{}, &models.TranscodeJob{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}
	return db
}

func createJobsTestFile(t *testing.T, db *gorm.DB, userID uint, filename, mime string) *models.File {
	t.Helper()
	file := &models.File{
		UserID:           userID,
		StoragePath:      "path-" + filename,
		LogicalPath:      "/",
		Filename:         filename,
		OriginalFilename: filename,
		FileSize:         100,
		MimeType:         mime,
		UploadStatus:     "completed",
	}
	if err := db.Create(file).Error; err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	return file
}

func TestEnqueueCreatesJobAndMarksPending(t *testing.T) {
	db := newJobsTestDB(t)
	user := &models.User{Username: "u", Email: "u@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	file := createJobsTestFile(t, db, user.ID, "movie.mkv", "video/x-matroska")

	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	var job models.TranscodeJob
	if err := db.Where("file_id = ?", file.ID).First(&job).Error; err != nil {
		t.Fatalf("job row not found: %v", err)
	}
	if job.Status != JobPending || job.UserID != user.ID {
		t.Errorf("job = %+v", job)
	}

	var reloaded models.File
	if err := db.First(&reloaded, file.ID).Error; err != nil {
		t.Fatalf("file not found: %v", err)
	}
	if reloaded.TranscodeStatus != StatusPending {
		t.Errorf("file status = %q, want pending", reloaded.TranscodeStatus)
	}
}

func TestEnqueueIsIdempotent(t *testing.T) {
	db := newJobsTestDB(t)
	user := &models.User{Username: "u", Email: "u@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	file := createJobsTestFile(t, db, user.ID, "movie.mkv", "video/x-matroska")

	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("first Enqueue failed: %v", err)
	}
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("second Enqueue failed: %v", err)
	}

	var count int64
	db.Model(&models.TranscodeJob{}).Where("file_id = ?", file.ID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 job row, got %d", count)
	}
}

func TestEnqueueRequeuesFailedJob(t *testing.T) {
	db := newJobsTestDB(t)
	user := &models.User{Username: "u", Email: "u@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	file := createJobsTestFile(t, db, user.ID, "movie.mkv", "video/x-matroska")

	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	now := time.Now()
	if err := db.Model(&models.TranscodeJob{}).Where("file_id = ?", file.ID).Updates(map[string]interface{}{
		"status":      JobFailed,
		"error":       "boom",
		"attempts":    3,
		"finished_at": now,
	}).Error; err != nil {
		t.Fatalf("failed to mark job failed: %v", err)
	}
	if err := db.Model(&models.File{}).Where("id = ?", file.ID).Updates(map[string]interface{}{
		"transcode_status": StatusFailed,
		"transcode_error":  "boom",
	}).Error; err != nil {
		t.Fatalf("failed to mark file failed: %v", err)
	}

	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("re-Enqueue failed: %v", err)
	}

	var job models.TranscodeJob
	if err := db.Where("file_id = ?", file.ID).First(&job).Error; err != nil {
		t.Fatalf("job row not found: %v", err)
	}
	if job.Status != JobPending || job.Attempts != 0 || job.Error != "" {
		t.Errorf("job not reset: %+v", job)
	}

	var reloaded models.File
	db.First(&reloaded, file.ID)
	if reloaded.TranscodeStatus != StatusPending {
		t.Errorf("file status = %q, want pending", reloaded.TranscodeStatus)
	}
}

func TestEnqueueSkipsCompletedFile(t *testing.T) {
	db := newJobsTestDB(t)
	user := &models.User{Username: "u", Email: "u@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	file := createJobsTestFile(t, db, user.ID, "movie.mkv", "video/x-matroska")
	if err := db.Model(file).Updates(map[string]interface{}{
		"video_variant_path": "variant.mp4",
		"transcode_status":   StatusCompleted,
	}).Error; err != nil {
		t.Fatalf("failed to mark file completed: %v", err)
	}

	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	var count int64
	db.Model(&models.TranscodeJob{}).Where("file_id = ?", file.ID).Count(&count)
	if count != 0 {
		t.Errorf("expected no job row, got %d", count)
	}
}

func TestClaimNextAndBackoff(t *testing.T) {
	db := newJobsTestDB(t)
	user := &models.User{Username: "u", Email: "u@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	file := createJobsTestFile(t, db, user.ID, "movie.mkv", "video/x-matroska")
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	job, err := ClaimNext(db)
	if err != nil {
		t.Fatalf("ClaimNext failed: %v", err)
	}
	if job == nil {
		t.Fatalf("expected a job to claim")
		return
	}
	if job.Status != JobProcessing || job.Attempts != 1 {
		t.Errorf("claimed job = %+v", job)
	}
	if job.StartedAt == nil {
		t.Error("expected started_at to be set")
	}

	// While processing, nothing else is claimable.
	if next, err := ClaimNext(db); err != nil || next != nil {
		t.Errorf("expected empty queue, got %+v, err %v", next, err)
	}

	// Future next_attempt_at blocks claiming.
	future := time.Now().Add(time.Hour)
	if err := db.Model(&models.TranscodeJob{}).Where("id = ?", job.ID).Updates(map[string]interface{}{
		"status":          JobPending,
		"next_attempt_at": future,
	}).Error; err != nil {
		t.Fatalf("failed to update job: %v", err)
	}
	if next, err := ClaimNext(db); err != nil || next != nil {
		t.Errorf("expected job to be blocked by backoff, got %+v, err %v", next, err)
	}

	// Past next_attempt_at allows claiming again.
	past := time.Now().Add(-time.Minute)
	if err := db.Model(&models.TranscodeJob{}).Where("id = ?", job.ID).Update("next_attempt_at", past).Error; err != nil {
		t.Fatalf("failed to update job: %v", err)
	}
	if next, err := ClaimNext(db); err != nil || next == nil {
		t.Fatalf("expected claimable job, got %+v, err %v", next, err)
	}
}

func TestRecoverStaleJobs(t *testing.T) {
	db := newJobsTestDB(t)
	user := &models.User{Username: "u", Email: "u@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	file := createJobsTestFile(t, db, user.ID, "movie.mkv", "video/x-matroska")
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	staleStart := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&models.TranscodeJob{}).Where("file_id = ?", file.ID).Updates(map[string]interface{}{
		"status":     JobProcessing,
		"started_at": staleStart,
	}).Error; err != nil {
		t.Fatalf("failed to mark job processing: %v", err)
	}
	if err := db.Model(&models.File{}).Where("id = ?", file.ID).Update("transcode_status", StatusProcessing).Error; err != nil {
		t.Fatalf("failed to mark file processing: %v", err)
	}

	if err := RecoverStaleJobs(db, 30*time.Minute); err != nil {
		t.Fatalf("RecoverStaleJobs failed: %v", err)
	}

	var job models.TranscodeJob
	db.Where("file_id = ?", file.ID).First(&job)
	if job.Status != JobPending || job.StartedAt != nil {
		t.Errorf("stale job not recovered: %+v", job)
	}

	var reloaded models.File
	db.First(&reloaded, file.ID)
	if reloaded.TranscodeStatus != StatusProcessing {
		t.Errorf("file still tied to a live job should stay processing, got %q", reloaded.TranscodeStatus)
	}
}

func TestRecoverOrphanedProcessingFile(t *testing.T) {
	db := newJobsTestDB(t)
	user := &models.User{Username: "u", Email: "u@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	file := createJobsTestFile(t, db, user.ID, "movie.mkv", "video/x-matroska")
	if err := db.Model(&models.File{}).Where("id = ?", file.ID).Update("transcode_status", StatusProcessing).Error; err != nil {
		t.Fatalf("failed to mark file processing: %v", err)
	}

	if err := RecoverStaleJobs(db, 30*time.Minute); err != nil {
		t.Fatalf("RecoverStaleJobs failed: %v", err)
	}

	var reloaded models.File
	db.First(&reloaded, file.ID)
	if reloaded.TranscodeStatus != StatusNone {
		t.Errorf("orphaned file status = %q, want none", reloaded.TranscodeStatus)
	}
}

func TestBackfill(t *testing.T) {
	db := newJobsTestDB(t)
	user := &models.User{Username: "u", Email: "u@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	video1 := createJobsTestFile(t, db, user.ID, "movie.mkv", "video/x-matroska")
	video2 := createJobsTestFile(t, db, user.ID, "clip.mp4", "application/octet-stream")
	// Not a video, must be skipped.
	createJobsTestFile(t, db, user.ID, "notes.txt", "text/plain")
	// Trashed, must be skipped.
	trashed := createJobsTestFile(t, db, user.ID, "old.mov", "video/quicktime")
	now := time.Now()
	if err := db.Model(trashed).Update("trashed_at", now).Error; err != nil {
		t.Fatalf("failed to trash file: %v", err)
	}
	// Already has variant, must be skipped.
	done := createJobsTestFile(t, db, user.ID, "done.webm", "video/webm")
	if err := db.Model(done).Updates(map[string]interface{}{
		"video_variant_path": "v.mp4",
		"transcode_status":   StatusCompleted,
	}).Error; err != nil {
		t.Fatalf("failed to mark done: %v", err)
	}

	count, err := Backfill(db)
	if err != nil {
		t.Fatalf("Backfill failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 jobs, got %d", count)
	}

	var jobCount int64
	db.Model(&models.TranscodeJob{}).Where("file_id IN ?", []uint{video1.ID, video2.ID}).Count(&jobCount)
	if jobCount != 2 {
		t.Errorf("expected 2 job rows, got %d", jobCount)
	}
}
