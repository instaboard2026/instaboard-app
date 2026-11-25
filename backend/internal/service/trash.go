package service

import (
	"bytes"
	_ "embed"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

//go:embed trash.js
var trashScript string

// MoveToTrash moves files to the macOS Trash (like Finder's Move to Trash), so they can be put back.
// Files on a card go to that card's own Trash. Returns the paths that could not be moved.
func MoveToTrash(paths []string) (failed []string, err error) {
	if runtime.GOOS != "darwin" {
		return paths, errors.New("Move to Trash is only available on macOS")
	}
	if len(paths) == 0 {
		return nil, nil
	}
	args := append([]string{"-l", "JavaScript", "-e", trashScript}, paths...)
	var out bytes.Buffer
	cmd := exec.Command("osascript", args...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return paths, errors.New("could not move files to the Trash")
	}
	done := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if status, path, ok := strings.Cut(line, "\t"); ok && status == "ok" {
			done[path] = true
		}
	}
	for _, p := range paths {
		if !done[p] {
			failed = append(failed, p)
		}
	}
	return failed, nil
}

// MoveToTrashTracked is MoveToTrash plus where each moved file now lives (trashed path → new path).
// macOS does not let apps list a Trash folder, but a file can be checked by its exact name: if
// "<card>/.Trashes/<uid>/<name>" was free before and holds the file after, that is where it went.
// The new path is "" when it cannot be told (a file with the same name was already in the Trash).
func MoveToTrashTracked(paths []string) (moved map[string]string, failed []string, err error) {
	free := map[string]bool{}
	for _, p := range paths {
		if guess := TrashGuess(p); guess != "" {
			if _, err := os.Lstat(guess); errors.Is(err, os.ErrNotExist) {
				free[p] = true
			}
		}
	}
	failed, err = MoveToTrash(paths)
	if err != nil {
		return nil, failed, err
	}
	notMoved := map[string]bool{}
	for _, p := range failed {
		notMoved[p] = true
	}
	moved = map[string]string{}
	for _, p := range paths {
		if notMoved[p] {
			continue
		}
		moved[p] = ""
		if guess := TrashGuess(p); free[p] && guess != "" {
			if fi, err := os.Stat(guess); err == nil && fi.Mode().IsRegular() {
				moved[p] = guess
			}
		}
	}
	return moved, failed, nil
}

// TrashGuess is where macOS puts a card file when its name is free: "<card>/.Trashes/<uid>/<name>".
func TrashGuess(path string) string {
	mount := VolumeMountPoint(path)
	if mount == "" {
		return ""
	}
	return filepath.Join(mount, ".Trashes", strconv.Itoa(os.Getuid()), filepath.Base(path))
}
