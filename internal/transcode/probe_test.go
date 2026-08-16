package transcode

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestDecide(t *testing.T) {
	tests := []struct {
		name         string
		probe        ProbeResult
		hasFaststart bool
		maxHeight    int
		want         Decision
	}{
		{
			name:         "compliant mp4 with faststart skips",
			probe:        ProbeResult{Container: "mov,mp4,m4a,3gp,3g2,mj2", VideoCodec: "h264", AudioCodec: "aac", Width: 1280, Height: 720},
			hasFaststart: true,
			maxHeight:    720,
			want:         DecisionSkip,
		},
		{
			name:         "compliant mp4 without faststart remuxes",
			probe:        ProbeResult{Container: "mov,mp4,m4a,3gp,3g2,mj2", VideoCodec: "h264", AudioCodec: "aac", Width: 640, Height: 360},
			hasFaststart: false,
			maxHeight:    720,
			want:         DecisionRemux,
		},
		{
			name:         "h264 aac mkv remuxes",
			probe:        ProbeResult{Container: "matroska,webm", VideoCodec: "h264", AudioCodec: "aac", Width: 1280, Height: 720},
			hasFaststart: false,
			maxHeight:    720,
			want:         DecisionRemux,
		},
		{
			name:         "video without audio is compatible",
			probe:        ProbeResult{Container: "matroska,webm", VideoCodec: "h264", Width: 1920, Height: 800},
			hasFaststart: false,
			maxHeight:    720,
			want:         DecisionTranscode, // too tall
		},
		{
			name:         "non-h264 codec transcodes",
			probe:        ProbeResult{Container: "mov,mp4,m4a,3gp,3g2,mj2", VideoCodec: "hevc", AudioCodec: "aac", Width: 1280, Height: 720},
			hasFaststart: true,
			maxHeight:    720,
			want:         DecisionTranscode,
		},
		{
			name:         "non-aac audio transcodes",
			probe:        ProbeResult{Container: "mov,mp4,m4a,3gp,3g2,mj2", VideoCodec: "h264", AudioCodec: "dts", Width: 1280, Height: 720},
			hasFaststart: true,
			maxHeight:    720,
			want:         DecisionTranscode,
		},
		{
			name:         "portrait taller than cap transcodes",
			probe:        ProbeResult{Container: "mov,mp4,m4a,3gp,3g2,mj2", VideoCodec: "h264", AudioCodec: "aac", Width: 1080, Height: 1920},
			hasFaststart: true,
			maxHeight:    720,
			want:         DecisionTranscode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(&tt.probe, tt.hasFaststart, tt.maxHeight); got != tt.want {
				t.Errorf("Decide() = %v, want %v", got, tt.want)
			}
		})
	}
}

// buildMP4 assembles a minimal top-level MP4 box layout.
func buildMP4(boxes ...string) []byte {
	var out []byte
	for _, box := range boxes {
		payload := []byte(box)
		size := uint32(8 + len(payload))
		buf := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(buf[0:4], size)
		copy(buf[4:8], []byte(box[:4]))
		copy(buf[8:], payload)
		out = append(out, buf...)
	}
	return out
}

func writeMP4(t *testing.T, boxes ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.mp4")
	if err := os.WriteFile(path, buildMP4(boxes...), 0644); err != nil {
		t.Fatalf("failed to write test mp4: %v", err)
	}
	return path
}

func TestMP4HasFaststart(t *testing.T) {
	// ftyp + moov before mdat => faststart
	faststartPath := writeMP4(t,
		"ftypisom",
		"moov",
		"mdat",
	)
	got, err := mp4HasFaststart(faststartPath)
	if err != nil {
		t.Fatalf("mp4HasFaststart failed: %v", err)
	}
	if !got {
		t.Error("expected faststart=true for moov-before-mdat layout")
	}

	// ftyp + mdat before moov => not faststart
	slowPath := writeMP4(t,
		"ftypisom",
		"mdat",
		"moov",
	)
	got, err = mp4HasFaststart(slowPath)
	if err != nil {
		t.Fatalf("mp4HasFaststart failed: %v", err)
	}
	if got {
		t.Error("expected faststart=false for mdat-before-moov layout")
	}

	// Non-MP4 garbage returns false, not an error.
	garbagePath := filepath.Join(t.TempDir(), "garbage.bin")
	if err := os.WriteFile(garbagePath, []byte("this is not a video"), 0644); err != nil {
		t.Fatalf("failed to write garbage file: %v", err)
	}
	got, err = mp4HasFaststart(garbagePath)
	if err != nil {
		t.Fatalf("mp4HasFaststart should not error on garbage: %v", err)
	}
	if got {
		t.Error("expected faststart=false for garbage input")
	}

	// Missing file errors.
	if _, err := mp4HasFaststart(filepath.Join(t.TempDir(), "missing.mp4")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestProbeVideo(t *testing.T) {
	ffprobePath := writeFakeProbe(t, `{"format":{"format_name":"matroska,webm"},"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080},{"codec_type":"audio","codec_name":"aac"}]}`)

	probe, err := ProbeVideo(context.Background(), ffprobePath, "input.mkv")
	if err != nil {
		t.Fatalf("ProbeVideo failed: %v", err)
	}
	if probe.Container != "matroska,webm" {
		t.Errorf("Container = %q", probe.Container)
	}
	if probe.VideoCodec != "h264" || probe.AudioCodec != "aac" {
		t.Errorf("codecs = %q/%q", probe.VideoCodec, probe.AudioCodec)
	}
	if probe.Width != 1920 || probe.Height != 1080 {
		t.Errorf("dimensions = %dx%d", probe.Width, probe.Height)
	}

	// No video stream => error.
	audioOnly := writeFakeProbe(t, `{"format":{"format_name":"mp3"},"streams":[{"codec_type":"audio","codec_name":"mp3"}]}`)
	if _, err := ProbeVideo(context.Background(), audioOnly, "input.mp3"); err == nil {
		t.Error("expected error for audio-only input")
	}

	// Failing ffprobe => error.
	badProbe := writeFakeScript(t, "ffprobe", "echo 'boom' >&2; exit 1")
	if _, err := ProbeVideo(context.Background(), badProbe, "input.mkv"); err == nil {
		t.Error("expected error for failing ffprobe")
	}
}
