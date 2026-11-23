package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	thumbWidth   = 360
	thumbTimeout = 45 * time.Second
	maxThumbJobs = 6
)

var thumbSem = make(chan struct{}, maxThumbJobs)

// ThumbCacheDir is {downloadDir}/camera-thumbs/{userID}.
func ThumbCacheDir(downloadDir string, userID int64) string {
	return filepath.Join(downloadDir, "camera-thumbs", strconv.FormatInt(userID, 10))
}

func thumbCacheKey(relPath string, size, mtimeNs int64) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|%d|%d", relPath, size, mtimeNs)
	return hex.EncodeToString(h.Sum(nil))
}

func isCameraStill(path string) bool {
	return MediaType(path) != "video"
}

// EnsureCameraThumb returns a path to a JPEG thumbnail, generating it if needed.
// Stills prefer macOS sips (fast); videos and failures fall back to ffmpeg.
func EnsureCameraThumb(ctx context.Context, downloadDir string, userID int64, srcPath, relPath string, size, mtimeNs int64) (string, error) {
	dir := ThumbCacheDir(downloadDir, userID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, thumbCacheKey(relPath, size, mtimeNs)+".jpg")
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return dest, nil
	}

	select {
	case thumbSem <- struct{}{}:
		defer func() { <-thumbSem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, thumbTimeout)
	defer cancel()

	tmp := dest + ".tmp.jpg"
	_ = os.Remove(tmp)

	var genErr error
	if isCameraStill(srcPath) || isCameraStill(relPath) {
		if err := generateThumbSips(ctx, srcPath, tmp); err == nil {
			if err := os.Rename(tmp, dest); err != nil {
				_ = os.Remove(tmp)
				return "", err
			}
			return dest, nil
		} else {
			genErr = err
			_ = os.Remove(tmp)
		}
	}

	if err := generateThumbFFmpeg(ctx, srcPath, tmp); err != nil {
		if genErr != nil {
			return "", fmt.Errorf("thumb: sips: %v; ffmpeg: %w", genErr, err)
		}
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

func generateThumbSips(ctx context.Context, srcPath, dest string) error {
	bin := toolPath("sips")
	if bin == "" {
		if p, err := exec.LookPath("sips"); err == nil {
			bin = p
		} else if _, err := os.Stat("/usr/bin/sips"); err == nil {
			bin = "/usr/bin/sips"
		} else {
			return fmt.Errorf("sips not found")
		}
	}
	// -Z fits longest edge; force JPEG for browser-safe thumbs (incl. HEIC).
	cmd := exec.CommandContext(ctx, bin,
		"-Z", strconv.Itoa(thumbWidth),
		"-s", "format", "jpeg",
		srcPath,
		"--out", dest,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(out)))
	}
	fi, err := os.Stat(dest)
	if err != nil || fi.Size() == 0 {
		return fmt.Errorf("sips produced empty output")
	}
	return nil
}

func generateThumbFFmpeg(ctx context.Context, srcPath, dest string) error {
	bin := toolPath("ffmpeg")
	if bin == "" {
		return fmt.Errorf("ffmpeg is not installed")
	}
	// -ss before -i for fast seek on videos; works for images too.
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-ss", "0.5",
		"-i", srcPath,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:-2", thumbWidth),
		"-q:v", "4",
		"-y", dest,
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(dest)
		args = []string{
			"-hide_banner", "-loglevel", "error",
			"-i", srcPath,
			"-frames:v", "1",
			"-vf", fmt.Sprintf("scale=%d:-2", thumbWidth),
			"-q:v", "4",
			"-y", dest,
		}
		cmd = exec.CommandContext(ctx, bin, args...)
		out, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg thumb: %w (%s)", err, string(out))
		}
	}
	return nil
}

// thumbGenOnce prevents stampeding the same key while generating.
var thumbInflight sync.Map

// EnsureCameraThumbSingleFlight wraps EnsureCameraThumb so concurrent requests share one encode.
func EnsureCameraThumbSingleFlight(ctx context.Context, downloadDir string, userID int64, srcPath, relPath string, size, mtimeNs int64) (string, error) {
	key := fmt.Sprintf("%d:%s:%d:%d", userID, relPath, size, mtimeNs)
	if v, ok := thumbInflight.Load(key); ok {
		ch := v.(chan struct{})
		select {
		case <-ch:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		dir := ThumbCacheDir(downloadDir, userID)
		dest := filepath.Join(dir, thumbCacheKey(relPath, size, mtimeNs)+".jpg")
		if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
			return dest, nil
		}
	}
	ch := make(chan struct{})
	if actual, loaded := thumbInflight.LoadOrStore(key, ch); loaded {
		wait := actual.(chan struct{})
		select {
		case <-wait:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		dir := ThumbCacheDir(downloadDir, userID)
		dest := filepath.Join(dir, thumbCacheKey(relPath, size, mtimeNs)+".jpg")
		if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
			return dest, nil
		}
	}
	defer func() {
		close(ch)
		thumbInflight.Delete(key)
	}()
	return EnsureCameraThumb(ctx, downloadDir, userID, srcPath, relPath, size, mtimeNs)
}
