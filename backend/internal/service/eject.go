package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Files the server is currently streaming from a card. Eject closes them first: a browser can keep
// a video download open long after the player is gone, and macOS waits for it before ejecting.
var cardFiles = struct {
	sync.Mutex
	open map[*os.File]struct{}
}{open: map[*os.File]struct{}{}}

// TrackCardFile registers a file being served from a card; call the returned func when done.
func TrackCardFile(f *os.File) func() {
	cardFiles.Lock()
	cardFiles.open[f] = struct{}{}
	cardFiles.Unlock()
	return func() {
		cardFiles.Lock()
		delete(cardFiles.open, f)
		cardFiles.Unlock()
	}
}

// closeCardFilesUnder closes every tracked file on the volume at mount (in-flight responses end).
func closeCardFilesUnder(mount string) int {
	cardFiles.Lock()
	defer cardFiles.Unlock()
	n := 0
	for f := range cardFiles.open {
		if strings.HasPrefix(f.Name(), mount+"/") {
			_ = f.Close()
			delete(cardFiles.open, f)
			n++
		}
	}
	return n
}

// ErrNotRemovable means the camera folder is on the Mac's own disk, so there is nothing to eject.
var ErrNotRemovable = errors.New("this folder is on your Mac's own disk, not a card")

// ErrVolumeBusy means macOS refused to eject because something still has files open on the card.
var ErrVolumeBusy = errors.New("the card is busy — close Finder windows or apps using it, wait for copies to finish, and try again")

// VolumeMountPoint returns "/Volumes/<name>" for a path on an external volume, or "" otherwise.
func VolumeMountPoint(path string) string {
	if runtime.GOOS != "darwin" || !filepath.IsAbs(path) {
		return ""
	}
	clean := filepath.Clean(path)
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	if len(parts) < 2 || parts[0] != "Volumes" || parts[1] == "" {
		return ""
	}
	return "/" + parts[0] + "/" + parts[1]
}

// EjectVolume safely ejects the volume mounted at mount (like Finder's Eject).
func EjectVolume(ctx context.Context, mount string) error {
	if mount == "" {
		return ErrNotRemovable
	}
	if closeCardFilesUnder(mount) > 0 {
		time.Sleep(150 * time.Millisecond) // let the cut-off responses unwind
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/diskutil", "eject", mount).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.ToLower(string(out))
	if strings.Contains(msg, "dissent") || strings.Contains(msg, "busy") || strings.Contains(msg, "in use") {
		return ErrVolumeBusy
	}
	if text := strings.TrimSpace(string(out)); text != "" {
		return errors.New(text)
	}
	return err
}
