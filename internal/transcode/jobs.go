package transcode

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/logger"
)

// Enqueue creates a transcode job for the given file if one isn't already
// queued or completed. It is idempotent: existing failed jobs are reset to
// pending (attempts cleared) so they can be retried.
func Enqueue(db *gorm.DB, fileID, userID uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var file models.File
		if err := tx.First(&file, fileID).Error; err != nil {
			return err
		}

		// Nothing to do if a variant already exists or transcoding finished.
		if file.VideoVariantPath != "" || file.TranscodeStatus == StatusCompleted {
			return nil
		}

		if err := tx.Model(&file).Updates(map[string]interface{}{
			"transcode_status": StatusPending,
			"transcode_error":  "",
		}).Error; err != nil {
			return err
		}

		job := models.TranscodeJob{
			FileID: fileID,
			UserID: userID,
			Status: JobPending,
		}

		// Upsert: reset an existing job so it can be retried.
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "file_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"status":          JobPending,
				"error":           "",
				"attempts":        0,
				"next_attempt_at": nil,
				"started_at":      nil,
				"finished_at":     nil,
				"updated_at":      time.Now(),
			}),
		}).Create(&job).Error
	})
}

// ClaimNext atomically claims the oldest pending job that is due for retry,
// marking it processing. It returns (nil, nil) when the queue is empty.
func ClaimNext(db *gorm.DB) (*models.TranscodeJob, error) {
	var job models.TranscodeJob

	err := db.Transaction(func(tx *gorm.DB) error {
		q := tx.Where("status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)",
			JobPending, time.Now()).Order("created_at").Limit(1)
		// SKIP LOCKED lets multiple workers claim distinct jobs concurrently
		// on Postgres; other backends fall back to plain row locking.
		if db.Dialector.Name() == "postgres" { // nolint:staticcheck // QF1008: db.Name() is not available on gorm.DB
			q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		} else {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := q.First(&job).Error; err != nil {
			return err
		}

		if err := tx.Model(&job).Updates(map[string]interface{}{
			"status":     JobProcessing,
			"started_at": time.Now(),
			"attempts":   gorm.Expr("attempts + 1"),
		}).Error; err != nil {
			return err
		}

		// Re-read so the caller sees the incremented attempt count.
		return tx.First(&job, job.ID).Error
	})
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// RecoverStaleJobs re-queues jobs stuck in "processing" (e.g. the worker was
// killed mid-transcode) and resets files stuck in "processing" whose job no
// longer exists.
func RecoverStaleJobs(db *gorm.DB, staleAge time.Duration) error {
	cutoff := time.Now().Add(-staleAge)

	result := db.Model(&models.TranscodeJob{}).
		Where("status = ? AND started_at < ?", JobProcessing, cutoff).
		Updates(map[string]interface{}{
			"status":     JobPending,
			"started_at": nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Warn("re-queued stale transcode jobs", "count", result.RowsAffected)
	}

	// Files stuck in "processing" without any live job become eligible again.
	var stuck []models.File
	if err := db.Where("transcode_status = ?", StatusProcessing).Find(&stuck).Error; err != nil {
		return err
	}
	for _, file := range stuck {
		var count int64
		if err := db.Model(&models.TranscodeJob{}).
			Where("file_id = ? AND status IN ?", file.ID, []string{JobPending, JobProcessing}).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := db.Model(&models.File{}).Where("id = ?", file.ID).
				Update("transcode_status", StatusNone).Error; err != nil {
				return err
			}
			logger.Warn("reset orphaned processing file", "file_id", file.ID)
		}
	}
	return nil
}

// Backfill enqueues transcode jobs for all completed, non-trashed video files
// that don't have a variant yet. Returns the number of jobs enqueued.
func Backfill(db *gorm.DB) (int, error) {
	var files []models.File
	if err := db.Where("upload_status = ? AND trashed_at IS NULL AND transcode_status IN ?",
		"completed", []string{StatusNone, StatusFailed}).
		Find(&files).Error; err != nil {
		return 0, err
	}

	count := 0
	for _, file := range files {
		if !IsVideoFile(file.MimeType, file.Filename) {
			continue
		}
		if err := Enqueue(db, file.ID, file.UserID); err != nil {
			logger.Error("failed to enqueue backfill job", "file_id", file.ID, "error", err)
			continue
		}
		logger.Info("backfilled transcode job", "file_id", file.ID, "filename", file.Filename)
		count++
	}
	return count, nil
}
