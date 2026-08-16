package transcode

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
)

// TranscodeOptions controls the ffmpeg H.264/AAC encode.
type TranscodeOptions struct {
	Preset    string // libx264 preset, e.g. "medium"
	CRF       int    // quality value, lower is better (18-28 typical)
	MaxHeight int    // output height cap, e.g. 720
	Threads   int    // -threads value (0 = let ffmpeg decide)
}

// Transcode re-encodes the input into a phone-friendly, web-optimized MP4:
// H.264 (max 1280x720), AAC audio, yuv420p, and a faststart moov atom.
func Transcode(ctx context.Context, ffmpegPath, inputPath, outputPath string, opts TranscodeOptions) error {
	vf := fmt.Sprintf(
		"scale=w='min(1280,iw)':h='min(%d,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2",
		opts.MaxHeight,
	)

	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", inputPath,
		"-map", "0:v:0", "-map", "0:a:0?", // first video stream + optional first audio stream
		"-sn", "-dn", // drop subtitle and data streams
		"-c:v", "libx264",
		"-preset", opts.Preset,
		"-crf", strconv.Itoa(opts.CRF),
		"-pix_fmt", "yuv420p",
		"-profile:v", "main",
		"-vf", vf,
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
		"-movflags", "+faststart",
		"-f", "mp4",
	}

	// Cap ffmpeg's thread count to limit CPU usage on shared hosts.
	if opts.Threads > 0 {
		args = append(args, "-threads", strconv.Itoa(opts.Threads))
	}
	args = append(args, outputPath)

	return runFFmpeg(ctx, ffmpegPath, args)
}

// Remux copies the existing streams into a faststart MP4 container without
// re-encoding. Used when the codecs are already web-compatible.
func Remux(ctx context.Context, ffmpegPath, inputPath, outputPath string) error {
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", inputPath,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-sn", "-dn",
		"-c", "copy",
		"-movflags", "+faststart",
		"-f", "mp4",
		outputPath,
	}

	return runFFmpeg(ctx, ffmpegPath, args)
}

func runFFmpeg(ctx context.Context, ffmpegPath string, args []string) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ffmpeg timed out")
		}
		return fmt.Errorf("ffmpeg failed: %w (output: %s)", err, out)
	}
	return nil
}
