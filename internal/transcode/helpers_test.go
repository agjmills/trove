package transcode

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFakeScript creates an executable shell script in a temp dir.
func writeFakeScript(t *testing.T, name, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatalf("failed to write fake script %s: %v", name, err)
	}
	return path
}

// writeFakeProbe creates a fake ffprobe binary that prints the given JSON.
func writeFakeProbe(t *testing.T, jsonOutput string) string {
	t.Helper()
	return writeFakeScript(t, "ffprobe", "cat <<'EOF'\n"+jsonOutput+"\nEOF\n")
}

// writeFakeFFmpeg creates a fake ffmpeg binary that writes a small output file
// (its last argument, matching real ffmpeg usage).
func writeFakeFFmpeg(t *testing.T, outputContent string) string {
	t.Helper()
	script := `for last; do :; done
printf '` + outputContent + `' > "$last"
`
	return writeFakeScript(t, "ffmpeg", script)
}
