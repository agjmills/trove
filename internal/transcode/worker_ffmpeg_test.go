package transcode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// These tests exercise the full worker pipeline against real ffmpeg/ffprobe
// binaries. They are skipped when ffmpeg is not installed (e.g. CI).

// requireFFmpeg skips the test when ffmpeg/ffprobe are unavailable.
func requireFFmpeg(t *testing.T) (string, string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not available")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not available")
	}
	return ffmpeg, ffprobe
}

// generateVideo creates a small test video using the real ffmpeg.
func generateVideo(t *testing.T, ffmpegPath, path string, size string, extraArgs ...string) {
	t.Helper()
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=" + size + ":rate=25",
	}
	args = append(args, extraArgs...)
	args = append(args, path)
	cmd := exec.Command(ffmpegPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate test video: %v (%s)", err, out)
	}
}

// newRealFFmpegEnv sets up a worker with real ffmpeg/ffprobe and disk storage.
func newRealFFmpegEnv(t *testing.T) (*gorm.DB, *models.User, *storage.DiskBackend, *config.Config, string) {
	t.Helper()
	ffmpeg, ffprobe := requireFFmpeg(t)

	dsn := fmt.Sprintf("file:realffmpeg-%d?mode=memory&cache=shared", time.Now().UnixNano())
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

	storageDir := filepath.Join(t.TempDir(), "storage")
	disk, err := storage.NewDiskBackend(storageDir)
	if err != nil {
		t.Fatalf("failed to create disk backend: %v", err)
	}
	t.Cleanup(func() { _ = disk.Close() })

	cfg := &config.Config{
		TempDir:               t.TempDir(),
		TranscodeTimeout:      2 * time.Minute,
		TranscodeMaxAttempts:  1,
		TranscodePollInterval: time.Second,
		TranscodeMaxHeight:    720,
		TranscodePreset:       "ultrafast",
		TranscodeCRF:          28,
		FFmpegPath:            ffmpeg,
		FFprobePath:           ffprobe,
		TranscodeStaleJobAge:  30 * time.Minute,
	}

	return db, user, disk, cfg, storageDir
}

func TestWorkerRealFFmpegTranscode(t *testing.T) {
	db, user, disk, cfg, storageDir := newRealFFmpegEnv(t)

	// 1280x800 source: taller than the 720p cap, so a re-encode is required.
	inputPath := filepath.Join(t.TempDir(), "input.mp4")
	generateVideo(t, cfg.FFmpegPath, inputPath, "1280x800",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", "-movflags", "+faststart")

	file := addWorkerFile(t, db, user, disk, "movie.mp4", "video/mp4", readTestFile(t, inputPath))
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	worker := NewWorker(db, cfg, disk)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	updated := reloadFile(t, db, file.ID)
	if updated.TranscodeStatus != StatusCompleted {
		t.Fatalf("file status = %q (error: %s)", updated.TranscodeStatus, updated.TranscodeError)
	}
	if updated.VideoVariantPath == file.StoragePath {
		t.Fatal("expected a new variant, not a reuse of the original")
	}

	variantPath := filepath.Join(storageDir, updated.VideoVariantPath)
	probe, err := ProbeVideo(context.Background(), cfg.FFprobePath, variantPath)
	if err != nil {
		t.Fatalf("failed to probe variant: %v", err)
	}
	if probe.VideoCodec != "h264" || probe.AudioCodec != "aac" {
		t.Errorf("variant codecs = %q/%q, want h264/aac", probe.VideoCodec, probe.AudioCodec)
	}
	if probe.Height > 720 {
		t.Errorf("variant height = %d, want <= 720", probe.Height)
	}
	if !strings.Contains(probe.Container, "mp4") {
		t.Errorf("variant container = %q, want mp4 family", probe.Container)
	}
	hasFaststart, err := mp4HasFaststart(variantPath)
	if err != nil || !hasFaststart {
		t.Errorf("variant not faststart-enabled (err: %v)", err)
	}
}

func TestWorkerRealFFmpegRemux(t *testing.T) {
	db, user, disk, cfg, storageDir := newRealFFmpegEnv(t)

	// 640x480 h264/aac MKV: compatible streams, remux should suffice.
	inputPath := filepath.Join(t.TempDir(), "input.mkv")
	generateVideo(t, cfg.FFmpegPath, inputPath, "640x480",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest")

	file := addWorkerFile(t, db, user, disk, "clip.mkv", "video/x-matroska", readTestFile(t, inputPath))
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	worker := NewWorker(db, cfg, disk)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	updated := reloadFile(t, db, file.ID)
	if updated.TranscodeStatus != StatusCompleted {
		t.Fatalf("file status = %q (error: %s)", updated.TranscodeStatus, updated.TranscodeError)
	}
	if updated.VideoVariantPath == file.StoragePath {
		t.Fatal("expected a new variant")
	}

	variantPath := filepath.Join(storageDir, updated.VideoVariantPath)
	probe, err := ProbeVideo(context.Background(), cfg.FFprobePath, variantPath)
	if err != nil {
		t.Fatalf("failed to probe variant: %v", err)
	}
	if probe.VideoCodec != "h264" || probe.AudioCodec != "aac" {
		t.Errorf("variant codecs = %q/%q, want h264/aac", probe.VideoCodec, probe.AudioCodec)
	}
	hasFaststart, err := mp4HasFaststart(variantPath)
	if err != nil || !hasFaststart {
		t.Errorf("variant not faststart-enabled (err: %v)", err)
	}
}

func TestWorkerRealFFmpegSkip(t *testing.T) {
	db, user, disk, cfg, _ := newRealFFmpegEnv(t)

	// Already a web-optimized 640x480 h264 faststart MP4.
	inputPath := filepath.Join(t.TempDir(), "input.mp4")
	generateVideo(t, cfg.FFmpegPath, inputPath, "640x480",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart")

	content := readTestFile(t, inputPath)
	file := addWorkerFile(t, db, user, disk, "movie.mp4", "video/mp4", content)
	if err := Enqueue(db, file.ID, user.ID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	worker := NewWorker(db, cfg, disk)
	if err := worker.Run(context.Background(), true); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	updated := reloadFile(t, db, file.ID)
	if updated.TranscodeStatus != StatusCompleted {
		t.Fatalf("file status = %q (error: %s)", updated.TranscodeStatus, updated.TranscodeError)
	}
	// The original object is reused as the variant.
	if updated.VideoVariantPath != file.StoragePath {
		t.Errorf("variant path = %q, want original %q", updated.VideoVariantPath, file.StoragePath)
	}

	// No extra quota charged for a reused object.
	updatedUser := reloadUser(t, db, user.ID)
	if updatedUser.StorageUsed != file.FileSize {
		t.Errorf("storage_used = %d, want %d", updatedUser.StorageUsed, file.FileSize)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}
