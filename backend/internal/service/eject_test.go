package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVolumeMountPoint(t *testing.T) {
	cases := map[string]string{
		"/Volumes/Untitled":                     "/Volumes/Untitled",
		"/Volumes/Untitled/PRIVATE/M4ROOT/CLIP": "/Volumes/Untitled",
		"/Volumes/My Card/DCIM/":                "/Volumes/My Card",
		"/Users/me/Movies/clips":                "",
		"/Volumes":                              "",
		"relative/Volumes/x":                    "",
	}
	for in, want := range cases {
		if got := VolumeMountPoint(in); got != want {
			t.Errorf("VolumeMountPoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCloseCardFilesUnderCutsOpenReads(t *testing.T) {
	dir := t.TempDir()
	onCard := filepath.Join(dir, "card", "clip.mp4")
	other := filepath.Join(dir, "cardigan", "clip.mp4") // same prefix text, different volume
	for _, p := range []string{onCard, other} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := os.Open(onCard)
	b, _ := os.Open(other)
	defer TrackCardFile(a)()
	defer TrackCardFile(b)()
	defer b.Close()

	if n := closeCardFilesUnder(filepath.Join(dir, "card")); n != 1 {
		t.Fatalf("closed %d files, want 1", n)
	}
	if _, err := a.Read(make([]byte, 1)); err == nil {
		t.Error("file on the ejected volume is still readable")
	}
	if _, err := b.Read(make([]byte, 1)); err != nil {
		t.Errorf("file on another volume was closed: %v", err)
	}
}
