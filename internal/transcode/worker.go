package transcode

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/storage"
)

// Worker polls the transcode job queue and processes one job at a time:
// download original -> probe -> transcode/remux -> store variant -> update DB.
type Worker struct {
	db          *gorm.DB
	cfg         *config.Config
	storage     storage.StorageBackend
	ffmpegPath  string
	ffprobePath string
}

// NewWorker creates a transcoding worker.
func NewWorker(db *gorm.DB, cfg *config.Config, storage storage.StorageBackend) *Worker {
	return &Worker{
		db:          db,
		cfg:         cfg,
		storage:     storage,
		ffmpegPath:  cfg.FFmpegPath,
		ffprobePath: cfg.FFprobePath,
	}
}

// Run recovers stale jobs and then continuously claims and processes pending
// jobs until the context is cancelled. With once=true it stops as soon as the
// queue is empty (useful for one-shot runs and tests).
func (w *Worker) Run(ctx context.Context, once bool) error {
	// Remove workspace directories left behind by a hard kill mid-transcode.
	if err := cleanupStaleTempDirs(w.cfg.TempDir, w.cfg.TranscodeStaleJobAge); err != nil {
		logger.Warn("failed to clean stale transcode temp dirs", "error", err)
	}

	if err := RecoverStaleJobs(w.db, w.cfg.TranscodeStaleJobAge); err != nil {
		logger.Error("failed to recover stale transcode jobs", "error", err)
	}

	interval := w.cfg.TranscodePollInterval
	for {
		job, err := ClaimNext(w.db)
		if err != nil {
			logger.Error("failed to claim transcode job", "error", err)
			if !w.sleep(ctx, interval) {
				return nil
			}
			continue
		}

		if job == nil {
			if once {
				return nil
			}
			if !w.sleep(ctx, interval) {
				return nil
			}
			continue
		}

		w.process(ctx, job)
	}
}

func (w *Worker) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// cleanupStaleTempDirs removes transcode workspace directories left behind by
// previous runs (e.g. the host was powered off mid-transcode and SIGKILL
// prevented the normal cleanup).
func cleanupStaleTempDirs(tempRoot string, olderThan time.Duration) error {
	if tempRoot == "" {
		return nil
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read temp root: %w", err)
	}

	cutoff := time.Now().Add(-olderThan)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "trove-transcode-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		dir := filepath.Join(tempRoot, entry.Name())
		if err := os.RemoveAll(dir); err != nil {
			logger.Warn("failed to remove stale transcode temp dir", "dir", dir, "error", err)
		} else {
			removed++
		}
	}
	if removed > 0 {
		logger.Info("removed stale transcode temp dirs", "count", removed)
	}
	return nil
}

// process handles a single claimed job. It never panics out of the loop.
func (w *Worker) process(ctx context.Context, job *models.TranscodeJob) {
	start := time.Now()
	logger.Info("transcode started", "job_id", job.ID, "file_id", job.FileID)

	if err := w.processJob(ctx, job); err != nil {
		logger.Error("transcode failed", "job_id", job.ID, "file_id", job.FileID, "error", err)
		w.fail(job, err)
		return
	}

	logger.Info("transcode completed",
		"job_id", job.ID,
		"file_id", job.FileID,
		"duration", time.Since(start).Round(time.Millisecond),
	)
}

// processJob performs the actual work for a claimed job.
func (w *Worker) processJob(ctx context.Context, job *models.TranscodeJob) error {
	// Load the file record; if it's gone, the job is orphaned.
	var file models.File
	if err := w.db.First(&file, job.FileID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			_ = w.db.Delete(&models.TranscodeJob{}, job.ID).Error
			return fmt.Errorf("file %d no longer exists, job discarded", job.FileID)
		}
		return fmt.Errorf("failed to load file %d: %w", job.FileID, err)
	}

	// Reflect processing state so the UI can show it.
	if err := w.db.Model(&file).Updates(map[string]interface{}{
		"transcode_status": StatusProcessing,
		"transcode_error":  "",
	}).Error; err != nil {
		return fmt.Errorf("failed to mark file processing: %w", err)
	}

	jobCtx, cancel := context.WithTimeout(ctx, w.cfg.TranscodeTimeout)
	defer cancel()

	// Work in a fresh temp directory (create the configured temp root first
	// in case it does not exist yet, e.g. on fresh deployments).
	if w.cfg.TempDir != "" {
		if err := os.MkdirAll(w.cfg.TempDir, 0700); err != nil {
			return fmt.Errorf("failed to create temp root: %w", err)
		}
	}
	tempDir, err := os.MkdirTemp(w.cfg.TempDir, "trove-transcode-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	// Download the original from storage to a local file for ffmpeg.
	inputExt := filepath.Ext(file.OriginalFilename)
	inputPath := filepath.Join(tempDir, "input"+inputExt)
	if err := w.download(jobCtx, file.StoragePath, inputPath); err != nil {
		return err
	}

	// Probe the source to decide what needs doing.
	probe, err := ProbeVideo(jobCtx, w.ffprobePath, inputPath)
	if err != nil {
		return fmt.Errorf("probe failed: %w", err)
	}
	logger.Info("video probed",
		"file_id", file.ID,
		"container", probe.Container,
		"video_codec", probe.VideoCodec,
		"audio_codec", probe.AudioCodec,
		"width", probe.Width,
		"height", probe.Height,
	)

	hasFaststart := false
	if probe.isMP4Container() {
		hasFaststart, err = mp4HasFaststart(inputPath)
		if err != nil {
			logger.Warn("failed to check faststart", "file_id", file.ID, "error", err)
		}
	}

	switch Decide(probe, hasFaststart, w.cfg.TranscodeMaxHeight) {
	case DecisionSkip:
		// Already a web-optimized MP4: stream the original directly.
		logger.Info("video already web-optimized, skipping transcode", "file_id", file.ID)
		return w.finish(job, file, file.StoragePath, file.FileSize, file.MimeType)

	case DecisionRemux:
		outputPath := filepath.Join(tempDir, "output.mp4")
		logger.Info("remuxing video into faststart MP4", "file_id", file.ID)
		if err := Remux(jobCtx, w.ffmpegPath, inputPath, outputPath); err != nil {
			logger.Warn("remux failed, falling back to full transcode", "file_id", file.ID, "error", err)
			return w.transcodeAndStore(jobCtx, job, file, inputPath, tempDir)
		}
		return w.storeVariant(jobCtx, job, file, outputPath)

	default:
		return w.transcodeAndStore(jobCtx, job, file, inputPath, tempDir)
	}
}

// transcodeAndStore runs the full H.264/AAC encode and stores the variant.
func (w *Worker) transcodeAndStore(ctx context.Context, job *models.TranscodeJob, file models.File, inputPath, tempDir string) error {
	outputPath := filepath.Join(tempDir, "output.mp4")
	logger.Info("transcoding video to H.264/AAC MP4", "file_id", file.ID)
	if err := Transcode(ctx, w.ffmpegPath, inputPath, outputPath, TranscodeOptions{
		Preset:    w.cfg.TranscodePreset,
		CRF:       w.cfg.TranscodeCRF,
		MaxHeight: w.cfg.TranscodeMaxHeight,
		Threads:   w.cfg.TranscodeThreads,
	}); err != nil {
		return err
	}
	return w.storeVariant(ctx, job, file, outputPath)
}

// storeVariant uploads the produced MP4 to storage and updates the DB.
func (w *Worker) storeVariant(ctx context.Context, job *models.TranscodeJob, file models.File, outputPath string) error {
	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("failed to stat output file: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("ffmpeg produced an empty output file")
	}

	outputFile, err := os.Open(outputPath)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer outputFile.Close() //nolint:errcheck

	base := strings.TrimSuffix(file.OriginalFilename, filepath.Ext(file.OriginalFilename))
	saveResult, err := w.storage.Save(ctx, outputFile, storage.SaveOptions{
		OriginalFilename: base + ".mp4",
		ContentType:      "video/mp4",
	})
	if err != nil {
		return fmt.Errorf("failed to store variant: %w", err)
	}

	if err := w.finish(job, file, saveResult.Path, saveResult.Size, "video/mp4"); err != nil {
		// Variant stored but DB update failed: remove the orphan.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if delErr := w.storage.Delete(cleanupCtx, saveResult.Path); delErr != nil {
			logger.Error("failed to remove orphaned variant", "path", saveResult.Path, "error", delErr)
		}
		return err
	}
	return nil
}

// finish records the variant on the file row, accounts for quota, and removes
// the job. The quota increment is skipped when the variant reuses the
// original object (DecisionSkip) so usage isn't double-counted.
func (w *Worker) finish(job *models.TranscodeJob, file models.File, variantPath string, variantSize int64, variantMime string) error {
	return w.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.File{}).Where("id = ?", file.ID).Updates(map[string]interface{}{
			"video_variant_path": variantPath,
			"video_variant_size": variantSize,
			"video_variant_mime": variantMime,
			"transcode_status":   StatusCompleted,
			"transcode_error":    "",
		}).Error; err != nil {
			return fmt.Errorf("failed to update file record: %w", err)
		}

		if variantPath != file.StoragePath {
			if err := tx.Model(&models.User{}).Where("id = ?", file.UserID).
				UpdateColumn("storage_used", gorm.Expr("storage_used + ?", variantSize)).Error; err != nil {
				return fmt.Errorf("failed to update user quota: %w", err)
			}
		}

		if err := tx.Delete(&models.TranscodeJob{}, job.ID).Error; err != nil {
			return fmt.Errorf("failed to remove job: %w", err)
		}
		return nil
	})
}

// fail either re-queues the job for a later attempt or, once the retry limit
// is reached, marks the file's transcode as permanently failed.
func (w *Worker) fail(job *models.TranscodeJob, err error) {
	message := err.Error()
	if len(message) > 500 {
		message = message[:497] + "..."
	}

	// Re-read the job to get the current attempt count.
	var current models.TranscodeJob
	if dbErr := w.db.First(&current, job.ID).Error; dbErr != nil {
		logger.Error("failed to reload job for failure handling", "job_id", job.ID, "error", dbErr)
		return
	}

	now := time.Now()
	if current.Attempts >= w.cfg.TranscodeMaxAttempts {
		logger.Error("transcode job failed permanently", "job_id", job.ID, "file_id", current.FileID, "attempts", current.Attempts, "error", message)
		_ = w.db.Transaction(func(tx *gorm.DB) error {
			if uErr := tx.Model(&models.File{}).Where("id = ?", current.FileID).Updates(map[string]interface{}{
				"transcode_status": StatusFailed,
				"transcode_error":  message,
			}).Error; uErr != nil {
				return uErr
			}
			return tx.Model(&models.TranscodeJob{}).Where("id = ?", current.ID).Updates(map[string]interface{}{
				"status":      JobFailed,
				"error":       message,
				"finished_at": now,
			}).Error
		})
		return
	}

	logger.Warn("transcode job will be retried",
		"job_id", job.ID,
		"file_id", current.FileID,
		"attempt", current.Attempts,
		"max_attempts", w.cfg.TranscodeMaxAttempts,
		"error", message,
	)
	// Exponential-ish backoff so persistent failures don't hot-loop.
	backoff := time.Duration(current.Attempts) * 30 * time.Second
	if backoff > 15*time.Minute {
		backoff = 15 * time.Minute
	}
	nextAttempt := now.Add(backoff)
	_ = w.db.Transaction(func(tx *gorm.DB) error {
		if uErr := tx.Model(&models.File{}).Where("id = ?", current.FileID).Updates(map[string]interface{}{
			"transcode_status": StatusPending,
			"transcode_error":  "",
		}).Error; uErr != nil {
			return uErr
		}
		return tx.Model(&models.TranscodeJob{}).Where("id = ?", current.ID).Updates(map[string]interface{}{
			"status":          JobPending,
			"error":           message,
			"next_attempt_at": nextAttempt,
			"finished_at":     now,
		}).Error
	})
}

// download copies a storage object to a local file.
func (w *Worker) download(ctx context.Context, storagePath, localPath string) error {
	reader, err := w.storage.Open(ctx, storagePath)
	if err != nil {
		return fmt.Errorf("failed to open original from storage: %w", err)
	}
	defer reader.Close() //nolint:errcheck

	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create temp input file: %w", err)
	}

	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to download original: %w", err)
	}
	return file.Close()
}
