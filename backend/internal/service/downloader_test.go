package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeGalleryDL writes a shell script that mimics gallery-dl: it creates the given files in the
// --directory argument, or prints stderr and exits 1 when no files are given.
func fakeGalleryDL(t *testing.T, stderr string, files ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake needs a Unix shell")
	}
	script := "#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = \"--directory\" ]; then dir=\"$2\"; fi; shift; done\n"
	for _, f := range files {
		script += "echo data > \"$dir/" + f + "\"\n"
	}
	if len(files) == 0 {
		script += "echo '" + stderr + "' >&2\nexit 1\n"
	}
	path := filepath.Join(t.TempDir(), "gallery-dl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDownloadReelSavedAsMP4(t *testing.T) {
	dir := t.TempDir()
	d := NewDownloader(fakeGalleryDL(t, "", "1.mp4"), "none", "/unused")

	state, err := d.Download(context.Background(), dir, 7, "https://instagram.com/reel/x")
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != DownloadKindVideo || state.Files != 1 {
		t.Fatalf("state = %+v", state)
	}
	if _, err := os.Stat(filepath.Join(dir, "7.mp4")); err != nil {
		t.Fatalf("7.mp4 missing: %v", err)
	}
	if got := d.Status(dir, 7); got == nil || got.Kind != DownloadKindVideo {
		t.Fatalf("Status = %+v", got)
	}
	assertNoTempDirs(t, dir)
}

func TestDownloadCarouselSavedAsFolder(t *testing.T) {
	dir := t.TempDir()
	d := NewDownloader(fakeGalleryDL(t, "", "1.jpg", "2.mp4", "3.jpg"), "none", "/unused")

	state, err := d.Download(context.Background(), dir, 9, "https://instagram.com/p/x")
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != DownloadKindPost || state.Files != 3 {
		t.Fatalf("state = %+v", state)
	}
	for _, f := range []string{"1.jpg", "2.mp4", "3.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, "9", f)); err != nil {
			t.Fatalf("%s missing: %v", f, err)
		}
	}
	if got := d.Status(dir, 9); got == nil || got.Files != 3 {
		t.Fatalf("Status = %+v", got)
	}
	assertNoTempDirs(t, dir)
}

func TestDiskUsage(t *testing.T) {
	dir := t.TempDir()
	d := NewDownloader("gallery-dl", "none", "/unused")
	if err := os.WriteFile(filepath.Join(dir, "4.mp4"), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "6"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{"1.jpg": 300, "2.mp4": 700, ".hidden": 999} {
		if err := os.WriteFile(filepath.Join(dir, "6", name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.DiskUsage(dir, 4); got != 1000 {
		t.Errorf("video usage = %d, want 1000", got)
	}
	if got := d.DiskUsage(dir, 6); got != 1000 {
		t.Errorf("post usage = %d, want 1000 (hidden files ignored)", got)
	}
	if got := d.DiskUsage(dir, 99); got != 0 {
		t.Errorf("missing usage = %d, want 0", got)
	}
}

func TestDownloadReplacesEarlierDownload(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "5.mp4"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewDownloader(fakeGalleryDL(t, "", "1.jpg", "2.jpg"), "none", "/unused")
	if _, err := d.Download(context.Background(), dir, 5, "https://instagram.com/p/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "5.mp4")); !os.IsNotExist(err) {
		t.Fatalf("old 5.mp4 should be removed, err = %v", err)
	}
	if got := d.Status(dir, 5); got == nil || got.Kind != DownloadKindPost {
		t.Fatalf("Status = %+v", got)
	}
}

func TestDownloadLoginRequired(t *testing.T) {
	dir := t.TempDir()
	d := NewDownloader(fakeGalleryDL(t, "[instagram][error] HTTP redirect to login page (https://www.instagram.com/accounts/login/)"), "none", "/unused")

	_, err := d.Download(context.Background(), dir, 3, "https://instagram.com/reel/x")
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
	if d.Status(dir, 3) != nil {
		t.Fatal("nothing should be marked downloaded")
	}
	assertNoTempDirs(t, dir)
}

func TestDownloadToolMissing(t *testing.T) {
	d := NewDownloader("/nonexistent/gallery-dl", "none", "/unused")
	if _, err := d.Download(context.Background(), t.TempDir(), 1, "https://instagram.com/reel/x"); !errors.Is(err, ErrToolMissing) {
		t.Fatalf("err = %v, want ErrToolMissing", err)
	}
}

func TestEngagementFromMediaInfoJSON(t *testing.T) {
	raw := []byte(`{
		"items": [{
			"like_count": 52,
			"comment_count": 7,
			"media_repost_count": 6,
			"reshare_count": 37,
			"play_count": 2345,
			"ig_play_count": 2345
		}],
		"status": "ok"
	}`)
	eng := engagementFromMediaInfoJSON(raw)
	if eng == nil {
		t.Fatal("expected engagement")
	}
	if eng.LikeCount == nil || *eng.LikeCount != 52 {
		t.Fatalf("likes = %v", eng.LikeCount)
	}
	if eng.CommentCount == nil || *eng.CommentCount != 7 {
		t.Fatalf("comments = %v", eng.CommentCount)
	}
	if eng.RepostCount == nil || *eng.RepostCount != 6 {
		t.Fatalf("reposts = %v", eng.RepostCount)
	}
	if eng.ShareCount == nil || *eng.ShareCount != 37 {
		t.Fatalf("shares = %v", eng.ShareCount)
	}
	if eng.PlayCount == nil || *eng.PlayCount != 2345 {
		t.Fatalf("plays = %v", eng.PlayCount)
	}
}

func TestParseEngagementFromPages(t *testing.T) {
	dir := t.TempDir()
	name := "01_https_www.instagram.com_api_v1_media_123_info_.txt"
	raw := `{"items":[{"like_count":10,"comment_count":2,"media_repost_count":1,"reshare_count":3,"ig_play_count":99}],"status":"ok"}`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := parseEngagementFromPages(dir)
	if eng == nil || eng.LikeCount == nil || *eng.LikeCount != 10 {
		t.Fatalf("engagement = %+v", eng)
	}
	removeGalleryDLPages(dir)
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Fatalf("page dump should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "1.mp4")); err != nil {
		t.Fatalf("media should remain: %v", err)
	}
}

func TestDetectYTDLModule(t *testing.T) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}
	mod := detectYTDLModule()
	if _, err := os.Stat(filepath.Join(mod, "__init__.py")); err != nil {
		t.Fatalf("detected module %q is not a yt_dlp package: %v", mod, err)
	}
}

func assertNoTempDirs(t *testing.T, dir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	if len(matches) > 0 {
		t.Fatalf("temp folders left behind: %v", matches)
	}
}
