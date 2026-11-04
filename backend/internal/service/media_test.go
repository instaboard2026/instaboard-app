package service

import (
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestMediaSignerRoundTrip(t *testing.T) {
	m := NewMediaSigner("secret", "http://localhost:8788/", time.Hour)
	items := m.Items(2, 7, []string{"7.mp4", "1.jpg"})
	if len(items) != 2 || items[0].Type != "video" || items[1].Type != "image" {
		t.Fatalf("items = %+v", items)
	}
	u, err := url.Parse(items[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/media/2/7/7.mp4" {
		t.Fatalf("path = %s", u.Path)
	}
	exp, sig := u.Query().Get("exp"), u.Query().Get("sig")
	if !m.Verify(2, 7, path.Base(u.Path), exp, sig) {
		t.Fatal("valid signature rejected")
	}
	if m.Verify(3, 7, "7.mp4", exp, sig) || m.Verify(2, 8, "7.mp4", exp, sig) || m.Verify(2, 7, "1.jpg", exp, sig) {
		t.Fatal("signature must bind user, save and file")
	}
	if NewMediaSigner("other", "", time.Hour).Verify(2, 7, "7.mp4", exp, sig) {
		t.Fatal("signature from another secret accepted")
	}
	past := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	if m.Verify(2, 7, "7.mp4", past, m.signature(2, 7, "7.mp4", time.Now().Add(-time.Minute).Unix())) {
		t.Fatal("expired link accepted")
	}
}

func TestMediaPathRejectsTraversal(t *testing.T) {
	d := NewDownloader("gallery-dl", "none", "/unused")
	dir := "/downloads"
	if got := d.MediaPath(dir, 7, "7.mp4"); got != filepath.Join(dir, "7.mp4") {
		t.Fatalf("video path = %s", got)
	}
	if got := d.MediaPath(dir, 9, "2.jpg"); got != filepath.Join(dir, "9", "2.jpg") {
		t.Fatalf("post path = %s", got)
	}
	for _, bad := range []string{"", "../secret", "a/b.jpg", ".hidden", ".."} {
		if got := d.MediaPath(dir, 9, bad); got != "" {
			t.Errorf("MediaPath(%q) = %q, want rejected", bad, got)
		}
	}
}
