package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	ErrPickerUnsupported = errors.New("the folder picker is only available on macOS")
	ErrPickerCancelled   = errors.New("no folder was chosen")
	ErrPickerBusy        = errors.New("a folder picker is already open on this computer")
	ErrFolderNotWritable = errors.New("can't save files in that folder. Choose another one")
)

const pickerTimeout = 5 * time.Minute

// pickerScript shows the standard macOS "Choose a folder" window in front of other apps.
// argv: prompt, optional default folder.
const pickerScript = `on run argv
	set promptText to item 1 of argv
	tell me to activate
	if (count of argv) > 1 then
		set chosen to choose folder with prompt promptText default location (POSIX file (item 2 of argv))
	else
		set chosen to choose folder with prompt promptText
	end if
	return POSIX path of chosen
end run`

var pickerMu sync.Mutex

// PickerAvailable reports whether ChooseFolder can open a native picker on this machine.
func PickerAvailable() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := exec.LookPath("osascript")
	return err == nil
}

// ChooseFolder opens the native macOS folder picker on the computer running the backend and returns
// the chosen absolute path. startDir is preselected when it exists.
func ChooseFolder(ctx context.Context, prompt, startDir string) (string, error) {
	if !PickerAvailable() {
		return "", ErrPickerUnsupported
	}
	if !pickerMu.TryLock() {
		return "", ErrPickerBusy
	}
	defer pickerMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, pickerTimeout)
	defer cancel()

	args := []string{"-e", pickerScript, prompt}
	if fi, err := os.Stat(startDir); err == nil && fi.IsDir() {
		args = append(args, startDir)
	}
	out, err := exec.CommandContext(ctx, "osascript", args...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil || strings.Contains(string(out), "-128") {
			return "", ErrPickerCancelled
		}
		return "", fmt.Errorf("folder picker failed: %s", strings.TrimSpace(string(out)))
	}
	path := filepath.Clean(strings.TrimSpace(string(out)))
	if err := CheckWritableDir(path); err != nil {
		return "", err
	}
	return path, nil
}

// CheckWritableDir verifies path is an existing folder we can create files in.
func CheckWritableDir(path string) error {
	if !filepath.IsAbs(path) {
		return ErrFolderNotWritable
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return ErrFolderNotWritable
	}
	f, err := os.CreateTemp(path, ".instaboard-write-test-")
	if err != nil {
		return ErrFolderNotWritable
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}
