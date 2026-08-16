package storage

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func testOpenRange(t *testing.T, backend StorageBackend) {
	t.Helper()
	ctx := context.Background()

	content := "0123456789abcdefghij"
	result, err := backend.Save(ctx, strings.NewReader(content), SaveOptions{
		OriginalFilename: "video.mp4",
		ContentType:      "video/mp4",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read the middle range.
	reader, err := backend.OpenRange(ctx, result.Path, 5, 8)
	if err != nil {
		t.Fatalf("OpenRange failed: %v", err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(data) != "56789abc" {
		t.Errorf("range content = %q, want %q", data, "56789abc")
	}

	// Range extending past EOF is clamped.
	reader, err = backend.OpenRange(ctx, result.Path, 18, 100)
	if err != nil {
		t.Fatalf("OpenRange failed: %v", err)
	}
	data, err = io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(data) != "ij" {
		t.Errorf("clamped range = %q, want %q", data, "ij")
	}

	// Full range from start.
	reader, err = backend.OpenRange(ctx, result.Path, 0, int64(len(content)))
	if err != nil {
		t.Fatalf("OpenRange failed: %v", err)
	}
	data, err = io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("full range = %q, want %q", data, content)
	}

	// Missing file.
	if _, err := backend.OpenRange(ctx, "missing.mp4", 0, 10); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Invalid length.
	if _, err := backend.OpenRange(ctx, result.Path, 0, 0); err == nil {
		t.Error("expected error for length 0")
	}
}

func TestDiskBackend_OpenRange(t *testing.T) {
	backend, err := NewDiskBackend(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatalf("NewDiskBackend failed: %v", err)
	}
	defer backend.Close() //nolint:errcheck
	testOpenRange(t, backend)
}

func TestMemoryBackend_OpenRange(t *testing.T) {
	testOpenRange(t, NewMemoryBackend())
}
