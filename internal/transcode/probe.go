package transcode

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// probeTimeout limits how long a single ffprobe run may take.
const probeTimeout = 60 * time.Second

// ProbeResult describes the media streams of a video file.
type ProbeResult struct {
	Container  string // ffmpeg format name, e.g. "matroska,webm"
	VideoCodec string // e.g. "h264"
	AudioCodec string // empty if no audio stream
	Width      int
	Height     int
}

// isMP4Container reports whether the probed container is in the MP4 family.
func (p *ProbeResult) isMP4Container() bool {
	for _, name := range strings.Split(p.Container, ",") {
		switch strings.TrimSpace(name) {
		case "mp4", "mov", "3gp", "3g2":
			return true
		}
	}
	return false
}

// ffprobeOutput mirrors the JSON structure emitted by ffprobe.
type ffprobeOutput struct {
	Format struct {
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

// ProbeVideo runs ffprobe on the given file and returns its media streams.
func ProbeVideo(ctx context.Context, ffprobePath, inputPath string) (*ProbeResult, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=format_name:stream=codec_type,codec_name,width,height",
		"-of", "json",
		inputPath,
	)
	out, err := cmd.Output()
	if err != nil {
		if probeCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("ffprobe timed out")
		}
		return nil, fmt.Errorf("ffprobe failed: %w (stderr: %s)", err, out)
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	result := &ProbeResult{Container: parsed.Format.FormatName}
	for _, stream := range parsed.Streams {
		switch stream.CodecType {
		case "video":
			if result.VideoCodec == "" {
				result.VideoCodec = stream.CodecName
				result.Width = stream.Width
				result.Height = stream.Height
			}
		case "audio":
			if result.AudioCodec == "" {
				result.AudioCodec = stream.CodecName
			}
		}
	}
	if result.VideoCodec == "" {
		return nil, fmt.Errorf("file contains no video stream")
	}
	return result, nil
}

// isWebCompatible reports whether the video is already H.264/AAC and fits
// within the target resolution, i.e. browsers can play it directly.
func (p *ProbeResult) isWebCompatible(maxHeight int) bool {
	if p.VideoCodec != "h264" {
		return false
	}
	if p.AudioCodec != "" && p.AudioCodec != "aac" {
		return false
	}
	return p.Width <= 1280 && p.Height <= maxHeight
}

// Decision describes what the worker should do with a video.
type Decision int

const (
	// DecisionTranscode means a full H.264/AAC re-encode is required.
	DecisionTranscode Decision = iota
	// DecisionRemux means the streams are already compatible and only need
	// remuxing into a faststart MP4 container.
	DecisionRemux
	// DecisionSkip means the file is already a faststart, web-compatible MP4;
	// the original can be streamed as-is.
	DecisionSkip
)

// Decide determines the cheapest operation that produces a web-optimized
// H.264/AAC MP4 variant from the probed file.
func Decide(probe *ProbeResult, hasFaststart bool, maxHeight int) Decision {
	if !probe.isWebCompatible(maxHeight) {
		return DecisionTranscode
	}
	if probe.isMP4Container() && hasFaststart {
		return DecisionSkip
	}
	// Compatible streams in a non-MP4 container (e.g. MKV) or an MP4 without
	// faststart: a stream-copy remux is enough.
	return DecisionRemux
}

// mp4HasFaststart scans the top-level boxes of an MP4-family file and reports
// whether the moov box appears before any mdat box (i.e. faststart-enabled).
// Unknown or unreadable box structures yield false so the caller falls back
// to a safe remux.
func mp4HasFaststart(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("failed to open file for moov check: %w", err)
	}
	defer f.Close() //nolint:errcheck

	header := make([]byte, 16)
	for {
		if _, err := io.ReadFull(f, header[:8]); err != nil {
			return false, nil // truncated or non-MP4 file
		}
		size := binary.BigEndian.Uint32(header[0:4])
		boxType := string(header[4:8])
		headerLen := int64(8)

		if size == 1 {
			// 64-bit extended size
			if _, err := io.ReadFull(f, header[8:16]); err != nil {
				return false, nil
			}
			extSize := binary.BigEndian.Uint64(header[8:16])
			headerLen = 16
			if extSize > uint64(^uint32(0)) {
				return false, nil // absurd size
			}
			size = uint32(extSize)
		}

		switch boxType {
		case "moov":
			return true, nil
		case "mdat":
			return false, nil
		}

		if size == 0 {
			return false, nil // extends to EOF: no trailing boxes to inspect
		}
		if int64(size) < headerLen {
			return false, nil
		}
		if _, err := f.Seek(int64(size)-headerLen, io.SeekCurrent); err != nil {
			return false, nil
		}
	}
}
