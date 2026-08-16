package transcode

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupStaleTempDirs(t *testing.T) {
	tempRoot := t.TempDir()

	staleDir := filepath.Join(tempRoot, "trove-transcode-stale")
	if err := os.MkdirAll(staleDir, 0700); err != nil {
		t.Fatalf("failed to create stale dir: %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(staleDir, oldTime, oldTime); err != nil {
		t.Fatalf("failed to backdate dir: %v", err)
	}

	freshDir := filepath.Join(tempRoot, "trove-transcode-fresh")
	if err := os.MkdirAll(freshDir, 0700); err != nil {
		t.Fatalf("failed to create fresh dir: %v", err)
	}

	unrelatedDir := filepath.Join(tempRoot, "other-dir")
	if err := os.MkdirAll(unrelatedDir, 0700); err != nil {
		t.Fatalf("failed to create unrelated dir: %v", err)
	}

	if err := cleanupStaleTempDirs(tempRoot, 30*time.Minute); err != nil {
		t.Fatalf("cleanupStaleTempDirs failed: %v", err)
	}

	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("stale dir should have been removed")
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Error("fresh dir should be kept")
	}
	if _, err := os.Stat(unrelatedDir); err != nil {
		t.Error("unrelated dir should be kept")
	}

	// Missing temp root is not an error.
	if err := cleanupStaleTempDirs(filepath.Join(tempRoot, "missing"), 30*time.Minute); err != nil {
		t.Errorf("missing temp root should not error: %v", err)
	}

	// Empty temp root is not an error.
	if err := cleanupStaleTempDirs("", 30*time.Minute); err != nil {
		t.Errorf("empty temp root should not error: %v", err)
	}
}
