package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Download kinds stored in the download folder.
const (
	DownloadKindVideo = "video" // <id>.mp4
	DownloadKindPost  = "post"  // <id>/1.jpg, <id>/2.mp4, ... (carousels and photo posts)
)

const downloadTimeout = 3 * time.Minute

var (
	ErrToolMissing     = errors.New("download tools aren't installed. Open Downloads in the footer and click Install")
	ErrLoginRequired   = errors.New("Instagram asked for a login. Log into instagram.com in Chrome on this computer, then try again")
	ErrRateLimited     = errors.New("Instagram is limiting requests right now. Wait a few minutes and try again")
	ErrPostNotFound    = errors.New("this post wasn't found. It may be deleted or private")
	ErrNothingFetched  = errors.New("nothing was downloaded from this link")
	ErrDownloadTimeout = errors.New("the download took too long and was stopped")
)

// DownloadState describes what's saved in the download folder for one save.
type DownloadState struct {
	Kind  string      `json:"kind"`
	Files int         `json:"files"`
	Media []MediaItem `json:"media,omitempty"`
}

// Downloader runs gallery-dl (which uses yt-dlp + ffmpeg for Instagram videos) and stores
// results in the given folder as <id>.mp4 for a single video, or <id>/<n>.<ext> for anything else.
type Downloader struct {
	bin            string
	cookiesBrowser string
	ytdlModule     string
	slots          chan struct{}
}

// NewDownloader creates a Downloader that runs one download at a time.
// cookiesBrowser is passed to --cookies-from-browser (e.g. "chrome"); "" or "none" disables it.
// ytdlModule is the yt_dlp package path for gallery-dl; "" means detect it from the yt-dlp command.
func NewDownloader(bin, cookiesBrowser, ytdlModule string) *Downloader {
	if strings.EqualFold(cookiesBrowser, "none") {
		cookiesBrowser = ""
	}
	if ytdlModule == "" {
		ytdlModule = detectYTDLModule()
	}
	return &Downloader{bin: bin, cookiesBrowser: cookiesBrowser, ytdlModule: ytdlModule, slots: make(chan struct{}, 1)}
}

// detectYTDLModule finds the yt_dlp package behind the installed yt-dlp command. Homebrew installs
// gallery-dl and yt-dlp in separate Python environments, so gallery-dl can't import yt_dlp on its own
// and would fail on Instagram's high-quality (DASH) videos.
func detectYTDLModule() string {
	exe := toolPath("yt-dlp")
	if exe == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 256)
	n, _ := f.Read(head)
	first := strings.SplitN(string(head[:n]), "\n", 2)[0]
	if !strings.HasPrefix(first, "#!") || !strings.Contains(first, "python") {
		return ""
	}
	python := strings.Fields(strings.TrimPrefix(first, "#!"))
	if len(python) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args := append(python[1:], "-c", "import os, yt_dlp; print(os.path.dirname(yt_dlp.__file__))")
	out, err := exec.CommandContext(ctx, python[0], args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// galleryDL returns the gallery-dl binary to run (managed ~/.instaboard/bin first, then PATH / config).
func (d *Downloader) galleryDL() string {
	// Explicit path from config/tests: use it only (don't fall back to a system install).
	if d.bin != "" && d.bin != "gallery-dl" && (filepath.IsAbs(d.bin) || strings.Contains(d.bin, string(os.PathSeparator))) {
		if fi, err := os.Stat(d.bin); err == nil && !fi.IsDir() {
			return d.bin
		}
		return ""
	}
	// In-app Uninstall sets tools-disabled so downloads stay off until Install, even if brew tools exist on PATH.
	if ToolsDisabled() {
		return ""
	}
	if p := toolPath("gallery-dl"); p != "" {
		return p
	}
	if d.bin != "" {
		if p, err := exec.LookPath(d.bin); err == nil {
			return p
		}
	}
	return ""
}

// Available reports whether gallery-dl can be found (managed install or on PATH).
func (d *Downloader) Available() bool {
	return d.galleryDL() != ""
}

// RefreshTools re-detects yt-dlp after an in-app install so downloads work without restarting.
func (d *Downloader) RefreshTools() {
	d.ytdlModule = detectYTDLModule()
}

// Status returns what's downloaded for a save in dir, or nil if nothing is.
func (d *Downloader) Status(dir string, id uint64) *DownloadState {
	name := strconv.FormatUint(id, 10)
	if fi, err := os.Stat(filepath.Join(dir, name+".mp4")); err == nil && fi.Mode().IsRegular() {
		return &DownloadState{Kind: DownloadKindVideo, Files: 1}
	}
	if files := listFiles(filepath.Join(dir, name)); len(files) > 0 {
		return &DownloadState{Kind: DownloadKindPost, Files: len(files)}
	}
	return nil
}

// MediaFiles lists a save's downloaded file names in display order: "<id>.mp4" for a video, or the
// files inside "<id>/" for a post.
func (d *Downloader) MediaFiles(dir string, id uint64) []string {
	name := strconv.FormatUint(id, 10)
	if fi, err := os.Stat(filepath.Join(dir, name+".mp4")); err == nil && fi.Mode().IsRegular() {
		return []string{name + ".mp4"}
	}
	return listFiles(filepath.Join(dir, name))
}

// DiskUsage returns the total size in bytes of a save's downloaded files in dir.
func (d *Downloader) DiskUsage(dir string, id uint64) int64 {
	name := strconv.FormatUint(id, 10)
	if fi, err := os.Stat(filepath.Join(dir, name+".mp4")); err == nil && fi.Mode().IsRegular() {
		return fi.Size()
	}
	var total int64
	postDir := filepath.Join(dir, name)
	for _, f := range listFiles(postDir) {
		if fi, err := os.Stat(filepath.Join(postDir, f)); err == nil {
			total += fi.Size()
		}
	}
	return total
}

// MediaPath returns the absolute path of a downloaded file of save id, or "" if file isn't a valid name.
func (d *Downloader) MediaPath(dir string, id uint64, file string) string {
	if file == "" || file != filepath.Base(file) || strings.HasPrefix(file, ".") {
		return ""
	}
	name := strconv.FormatUint(id, 10)
	if file == name+".mp4" {
		return filepath.Join(dir, file)
	}
	return filepath.Join(dir, name, file)
}

// Download fetches every photo/video of the post at url into dir for save id, replacing any earlier download.
func (d *Downloader) Download(ctx context.Context, dir string, id uint64, url string) (*DownloadState, error) {
	select {
	case d.slots <- struct{}{}:
		defer func() { <-d.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if !d.Available() {
		return nil, ErrToolMissing
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create download folder: %w", err)
	}
	removeStaleTempDirs(dir)
	tmp, err := os.MkdirTemp(dir, ".tmp-")
	if err != nil {
		return nil, fmt.Errorf("create temp folder: %w", err)
	}
	defer os.RemoveAll(tmp)

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	args := []string{"--directory", tmp, "--filename", "{num}.{extension}"}
	if d.cookiesBrowser != "" {
		args = append(args, "--cookies-from-browser", d.cookiesBrowser+"/.instagram.com")
	}
	if d.ytdlModule != "" {
		args = append(args, "-o", "downloader.ytdl.module="+d.ytdlModule)
	}
	args = append(args, "--", url)

	bin := d.galleryDL()
	if bin == "" {
		return nil, ErrToolMissing
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "PATH="+pathWithManagedBin())
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, ErrDownloadTimeout
	}

	files := listFiles(tmp)
	if len(files) == 0 {
		return nil, classifyGalleryDLError(stderr.String(), runErr)
	}

	name := strconv.FormatUint(id, 10)
	videoPath := filepath.Join(dir, name+".mp4")
	postDir := filepath.Join(dir, name)
	_ = os.Remove(videoPath)
	_ = os.RemoveAll(postDir)

	if len(files) == 1 && strings.EqualFold(filepath.Ext(files[0]), ".mp4") {
		if err := os.Rename(filepath.Join(tmp, files[0]), videoPath); err != nil {
			return nil, fmt.Errorf("move video: %w", err)
		}
		return &DownloadState{Kind: DownloadKindVideo, Files: 1}, nil
	}
	if err := os.Rename(tmp, postDir); err != nil {
		return nil, fmt.Errorf("move post files: %w", err)
	}
	return &DownloadState{Kind: DownloadKindPost, Files: len(files)}, nil
}

// removeStaleTempDirs deletes .tmp-* folders left behind by downloads interrupted by a crash or restart.
// Only folders older than the download timeout are removed, so a running download is never touched.
func removeStaleTempDirs(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.IsDir() && time.Since(fi.ModTime()) > 2*downloadTimeout {
			os.RemoveAll(m)
		}
	}
}

// listFiles returns the visible regular files in dir, sorted by name.
func listFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") && !strings.HasSuffix(e.Name(), ".part") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func classifyGalleryDLError(stderr string, runErr error) error {
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "login") || strings.Contains(s, "401") || strings.Contains(s, "checkpoint") || strings.Contains(s, "authentication"):
		return ErrLoginRequired
	case strings.Contains(s, "429") || strings.Contains(s, "rate limit") || strings.Contains(s, "please wait"):
		return ErrRateLimited
	case strings.Contains(s, "404") || strings.Contains(s, "not found") || strings.Contains(s, "notfound"):
		return ErrPostNotFound
	}
	if line := lastLine(stderr); line != "" {
		return fmt.Errorf("download failed: %s", line)
	}
	if runErr != nil {
		return fmt.Errorf("download failed: %w", runErr)
	}
	return ErrNothingFetched
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
