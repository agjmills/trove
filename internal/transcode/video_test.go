package transcode

import "testing"

func TestIsVideoFile(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		filename string
		want     bool
	}{
		{"video mime", "video/mp4", "movie.bin", true},
		{"video mime uppercase", "Video/MP4", "movie.bin", true},
		{"mkv extension", "application/octet-stream", "holiday.mkv", true},
		{"mov extension uppercase", "", "Clip.MOV", true},
		{"mts extension", "", "footage.mts", true},
		{"no extension video mime", "video/x-matroska", "file", true},
		{"plain text", "text/plain", "notes.txt", false},
		{"octet stream no ext", "application/octet-stream", "backup", false},
		{"audio mime", "audio/mpeg", "song.mp3", false},
		{"image mime", "image/png", "photo.mp4", true}, // extension beats nothing
		{"image png file", "image/png", "photo.png", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsVideoFile(tt.mimeType, tt.filename); got != tt.want {
				t.Errorf("IsVideoFile(%q, %q) = %v, want %v", tt.mimeType, tt.filename, got, tt.want)
			}
		})
	}
}
