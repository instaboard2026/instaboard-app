package service

import (
	"bytes"
	_ "embed"
	"errors"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

//go:embed airdrop.js
var airDropScript string

// ErrAirDropUnavailable means this Mac cannot AirDrop right now (off, no Wi-Fi/Bluetooth, or not macOS).
var ErrAirDropUnavailable = errors.New("AirDrop is not available on this Mac — turn on Wi-Fi and Bluetooth")

// OpenAirDrop opens the native macOS AirDrop window for files already on disk.
// Nothing is copied: AirDrop reads the files straight from their paths. It returns once the
// window is up; the helper process keeps running until the user sends or cancels.
func OpenAirDrop(paths []string) error {
	if runtime.GOOS != "darwin" {
		return ErrAirDropUnavailable
	}
	if len(paths) == 0 {
		return errors.New("no files to share")
	}
	args := append([]string{"-l", "JavaScript", "-e", airDropScript}, paths...)
	cmd := exec.Command("osascript", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case err := <-exited:
		// Exited straight away: AirDrop refused the items or the script failed.
		res := strings.TrimSpace(out.String())
		if strings.Contains(res, "unavailable") {
			return ErrAirDropUnavailable
		}
		if err != nil {
			slog.Error("airdrop helper failed", "err", err, "out", res)
			return errors.New("could not open AirDrop")
		}
		return nil
	case <-time.After(1500 * time.Millisecond):
		// Window is showing; log the outcome when the user finishes.
		go func() {
			<-exited
			slog.Info("airdrop finished", "files", len(paths), "result", strings.TrimSpace(out.String()))
		}()
		return nil
	}
}
