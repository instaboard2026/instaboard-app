package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/mod/semver"
)

// Updates are installed under ~/.instaboard/runtime/ so the double-clicked Instaboard
// binary the user already Allowed never changes — avoiding repeated Gatekeeper prompts.

func runtimeDir() string {
	return filepath.Join(dataHome(), "runtime")
}

// RuntimeBinaryPath is where Update-and-restart installs the new Instaboard executable.
func RuntimeBinaryPath() string {
	return filepath.Join(runtimeDir(), "Instaboard")
}

func runtimeVersionPath() string {
	return filepath.Join(runtimeDir(), "VERSION")
}

func launcherPathFile() string {
	return filepath.Join(runtimeDir(), "launcher-path")
}

func currentExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

func readRuntimeVersion() string {
	b, err := os.ReadFile(runtimeVersionPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeRuntimeVersion(v string) error {
	if err := os.MkdirAll(runtimeDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(runtimeVersionPath(), []byte(strings.TrimSpace(v)+"\n"), 0o600)
}

func recordLauncher(path string) {
	_ = os.MkdirAll(runtimeDir(), 0o700)
	_ = os.WriteFile(launcherPathFile(), []byte(path+"\n"), 0o600)
}

// LauncherPath returns the user-facing binary that should be restarted after an update
// (the file they originally Allowed), when known.
func LauncherPath() string {
	b, err := os.ReadFile(launcherPathFile())
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return ""
	}
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return ""
	}
	return p
}

// RelaunchExecutable is the binary path to start after Restart / Update-and-restart.
// Prefer the original launcher so Gatekeeper is not asked again for the runtime copy.
func RelaunchExecutable() (string, error) {
	if p := LauncherPath(); p != "" {
		return p, nil
	}
	return currentExePath()
}

// PrepareRuntimeBinary installs srcExe as the managed runtime binary for version.
func PrepareRuntimeBinary(srcExe, version string) error {
	if err := os.MkdirAll(runtimeDir(), 0o700); err != nil {
		return err
	}
	dest := RuntimeBinaryPath()
	tmp := dest + ".new"
	if err := copyFile(srcExe, tmp); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o755)
	clearMacDownloadAttrs(tmp)
	adhocCodesign(tmp)
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(dest)
		if err2 := os.Rename(tmp, dest); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
	}
	_ = os.Chmod(dest, 0o755)
	clearMacDownloadAttrs(dest)
	adhocCodesign(dest)
	return writeRuntimeVersion(version)
}

func clearMacDownloadAttrs(path string) {
	if runtime.GOOS != "darwin" {
		return
	}
	_ = exec.Command("xattr", "-cr", path).Run()
	_ = exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
}

func adhocCodesign(path string) {
	if runtime.GOOS != "darwin" {
		return
	}
	_ = exec.Command("codesign", "--force", "--sign", "-", "--timestamp=none", path).Run()
}

// MaybeExecRuntime switches to ~/.instaboard/runtime/Instaboard when that copy is
// newer than (or equal to) this process. The launcher file the user Allowed stays unchanged.
func MaybeExecRuntime(currentVersion string) {
	if os.Getenv("INSTABOARD_SKIP_RUNTIME") == "1" {
		return
	}
	self, err := currentExePath()
	if err != nil {
		return
	}
	runtimeBin := RuntimeBinaryPath()
	if sameFilePath(self, runtimeBin) {
		return
	}
	fi, err := os.Stat(runtimeBin)
	if err != nil || fi.IsDir() {
		return
	}
	runtimeVer := readRuntimeVersion()
	if runtimeVer == "" {
		return
	}
	cur := normalizeSemver(currentVersion)
	rt := normalizeSemver(runtimeVer)
	if !semver.IsValid(cur) || !semver.IsValid(rt) {
		return
	}
	// Prefer runtime when it is the same or newer. A newer launcher (manual zip) skips this.
	if semver.Compare(rt, cur) < 0 {
		return
	}
	recordLauncher(self)
	clearMacDownloadAttrs(runtimeBin)
	adhocCodesign(runtimeBin)

	env := os.Environ()
	if err := syscall.Exec(runtimeBin, append([]string{runtimeBin}, os.Args[1:]...), env); err != nil {
		fmt.Fprintf(os.Stderr, "instaboard: could not switch to updated runtime: %v\n", err)
	}
}

func sameFilePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(ai, bi)
}
