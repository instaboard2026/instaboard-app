package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckWritableDir(t *testing.T) {
	dir := t.TempDir()
	if err := CheckWritableDir(dir); err != nil {
		t.Fatalf("temp dir should be writable: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("write test file left behind: %v", entries)
	}

	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"relative/path", filepath.Join(dir, "missing"), file} {
		if err := CheckWritableDir(bad); !errors.Is(err, ErrFolderNotWritable) {
			t.Errorf("CheckWritableDir(%q) = %v, want ErrFolderNotWritable", bad, err)
		}
	}
}
