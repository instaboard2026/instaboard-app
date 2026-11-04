package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Managed tools live under ~/.instaboard so the app can install them without Homebrew,
// and so downloads work immediately after install (no app restart).

func dataHome() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".instaboard")
	}
	return ".instaboard"
}

// ManagedBinDir is where Instaboard keeps gallery-dl, yt-dlp and ffmpeg.
func ManagedBinDir() string {
	return filepath.Join(dataHome(), "bin")
}

func managedVenvDir() string {
	return filepath.Join(dataHome(), "venv")
}

func toolsDisabledPath() string {
	return filepath.Join(dataHome(), "tools-disabled")
}

// ToolsDisabled is true after the user uninstalls tools in the app (until they install again).
func ToolsDisabled() bool {
	_, err := os.Stat(toolsDisabledPath())
	return err == nil
}

func clearToolsDisabled() {
	_ = os.Remove(toolsDisabledPath())
}

// toolPath returns an executable path for name, preferring ~/.instaboard/bin.
func toolPath(name string) string {
	if ToolsDisabled() {
		return ""
	}
	managed := filepath.Join(ManagedBinDir(), name)
	if fi, err := os.Stat(managed); err == nil && !fi.IsDir() {
		return managed
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// pathWithManagedBin puts the managed bin directory first so ffmpeg is found by gallery-dl/yt-dlp.
func pathWithManagedBin() string {
	return ManagedBinDir() + string(os.PathListSeparator) + os.Getenv("PATH")
}

// InstallDownloadTools installs gallery-dl, yt-dlp and ffmpeg for Instaboard.
// Prefers Homebrew when available; otherwise uses a local Python venv + a static ffmpeg binary.
// After this returns successfully, Available() is true without restarting the process.
// Safe to call again for reinstall.
func InstallDownloadTools(ctx context.Context) error {
	clearToolsDisabled()
	if err := os.MkdirAll(ManagedBinDir(), 0o755); err != nil {
		return fmt.Errorf("create tools folder: %w", err)
	}
	if brew, err := exec.LookPath("brew"); err == nil {
		if err := brewInstallTools(ctx, brew); err != nil {
			return err
		}
		return linkBrewTools()
	}
	return pipInstallTools(ctx)
}

// UninstallDownloadTools removes Instaboard's managed tool copies and disables tools in the app
// until the user installs again (system Homebrew packages are left alone).
func UninstallDownloadTools() error {
	_ = os.RemoveAll(ManagedBinDir())
	_ = os.RemoveAll(managedVenvDir())
	if err := os.MkdirAll(dataHome(), 0o700); err != nil {
		return fmt.Errorf("prepare data folder: %w", err)
	}
	if err := os.WriteFile(toolsDisabledPath(), []byte("disabled by Instaboard\n"), 0o600); err != nil {
		return fmt.Errorf("disable tools: %w", err)
	}
	return nil
}

func brewInstallTools(ctx context.Context, brew string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, brew, "install", "gallery-dl", "yt-dlp", "ffmpeg")
	cmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("brew install failed: %s", truncate(msg, 400))
	}
	return nil
}

// linkBrewTools copies brew binaries into ~/.instaboard/bin when possible so the app
// always finds them first (and users without brew-on-PATH still work after install).
func linkBrewTools() error {
	for _, name := range []string{"gallery-dl", "yt-dlp", "ffmpeg"} {
		src, err := exec.LookPath(name)
		if err != nil {
			return fmt.Errorf("%s not found after brew install", name)
		}
		dst := filepath.Join(ManagedBinDir(), name)
		_ = os.Remove(dst)
		if err := os.Symlink(src, dst); err != nil {
			// Fall back to leaving it on PATH only.
			continue
		}
	}
	return nil
}

func pipInstallTools(ctx context.Context) error {
	python, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("Python 3 is required to install download tools (or install Homebrew and try again)")
	}
	venv := managedVenvDir()
	if _, err := os.Stat(filepath.Join(venv, "bin", "python3")); err != nil {
		cmd := exec.CommandContext(ctx, python, "-m", "venv", venv)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("create Python environment: %s", truncate(strings.TrimSpace(string(out)), 300))
		}
	}
	pip := filepath.Join(venv, "bin", "pip")
	ctxPip, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctxPip, pip, "install", "--upgrade", "gallery-dl", "yt-dlp")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pip install failed: %s", truncate(strings.TrimSpace(string(out)), 400))
	}
	for _, name := range []string{"gallery-dl", "yt-dlp"} {
		src := filepath.Join(venv, "bin", name)
		dst := filepath.Join(ManagedBinDir(), name)
		_ = os.Remove(dst)
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("link %s: %w", name, err)
		}
	}
	if toolPath("ffmpeg") != "" {
		// System ffmpeg is fine; prefer linking into managed bin when we found it on PATH.
		if src, err := exec.LookPath("ffmpeg"); err == nil {
			dst := filepath.Join(ManagedBinDir(), "ffmpeg")
			if _, err := os.Stat(dst); err != nil {
				_ = os.Symlink(src, dst)
			}
		}
		return nil
	}
	return installFFmpegBinary(ctx)
}

func installFFmpegBinary(ctx context.Context) error {
	url, err := ffmpegDownloadURL(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download ffmpeg: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download ffmpeg: HTTP %d", res.StatusCode)
	}
	tmpZip := filepath.Join(ManagedBinDir(), "ffmpeg-download.zip")
	f, err := os.Create(tmpZip)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		f.Close()
		_ = os.Remove(tmpZip)
		return fmt.Errorf("save ffmpeg: %w", err)
	}
	f.Close()
	defer os.Remove(tmpZip)

	dst := filepath.Join(ManagedBinDir(), "ffmpeg")
	if err := unzipNamedFile(tmpZip, "ffmpeg", dst); err != nil {
		// Some builds ship a bare binary (not a zip).
		if copyErr := copyFile(tmpZip, dst); copyErr != nil {
			return fmt.Errorf("extract ffmpeg: %w", err)
		}
	}
	return os.Chmod(dst, 0o755)
}

func ffmpegDownloadURL(ctx context.Context) (string, error) {
	// ffbinaries.com publishes static ffmpeg builds for macOS (and others).
	arch := "mac-64"
	if runtime.GOARCH == "arm64" {
		arch = "osx-64" // ffbinaries historically labels Intel; try mac-64 first below
	}
	_ = arch
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ffbinaries.com/api/v1/version/latest", nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ffmpeg catalog: %w", err)
	}
	defer res.Body.Close()
	var catalog struct {
		Bin map[string]struct {
			FFmpeg string `json:"ffmpeg"`
		} `json:"bin"`
	}
	if err := json.NewDecoder(res.Body).Decode(&catalog); err != nil {
		return "", fmt.Errorf("ffmpeg catalog: %w", err)
	}
	candidates := []string{"osx-64", "mac-64", "linux-64"}
	if runtime.GOARCH == "arm64" {
		candidates = []string{"osx-64", "mac-64"}
	}
	for _, key := range candidates {
		if b, ok := catalog.Bin[key]; ok && b.FFmpeg != "" {
			return b.FFmpeg, nil
		}
	}
	// Fallback: yt-dlp macOS-friendly evermeet build (Intel; runs under Rosetta on Apple Silicon).
	return "https://evermeet.cx/ffmpeg/getrelease/zip", nil
}

func unzipNamedFile(zipPath, name, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	var found *zip.File
	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base == name || base == name+".exe" {
			found = f
			break
		}
	}
	if found == nil && len(r.File) == 1 && !r.File[0].FileInfo().IsDir() {
		found = r.File[0]
	}
	if found == nil {
		return fmt.Errorf("%s not found in archive", name)
	}
	rc, err := found.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
