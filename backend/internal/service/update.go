package service

import (
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
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const (
	defaultUpdateRepo  = "instaboard2026/instaboard-app"
	defaultUpdateAsset = "Instaboard-mac.zip"
	updateCacheTTL     = time.Hour
)

// UpdateInfo is returned by GET /api/update.
type UpdateInfo struct {
	Current         string     `json:"current"`
	Latest          string     `json:"latest"`
	UpdateAvailable bool       `json:"update_available"`
	DownloadURL     string     `json:"download_url,omitempty"`
	Notes           string     `json:"notes,omitempty"`
	RepoURL         string     `json:"repo_url,omitempty"`
	ReleasesURL     string     `json:"releases_url,omitempty"` // https://github.com/.../releases — where users download
	ReleaseURL      string     `json:"release_url,omitempty"`  // specific latest release page
	PublishedAt     *time.Time `json:"published_at,omitempty"` // latest release publish time
	CheckedAt       time.Time  `json:"checked_at"`
	Skipped         bool       `json:"skipped,omitempty"` // true when version is "dev" or checks disabled
}

// UpdateDownloadResult is returned by POST /api/update/download.
type UpdateDownloadResult struct {
	Path string `json:"path"`
}

// Updater checks GitHub Releases and downloads the Mac zip.
type Updater struct {
	current string
	repo    string
	asset   string
	enabled bool

	mu    sync.Mutex
	cache *UpdateInfo
}

// NewUpdater creates an updater. current is the binary version (link-time -X main.version).
// UPDATE_CHECK=0 disables checks; UPDATE_REPO overrides owner/repo.
func NewUpdater(current string) *Updater {
	enabled := true
	switch strings.ToLower(strings.TrimSpace(os.Getenv("UPDATE_CHECK"))) {
	case "0", "false", "no":
		enabled = false
	}
	repo := strings.TrimSpace(os.Getenv("UPDATE_REPO"))
	if repo == "" {
		repo = defaultUpdateRepo
	}
	return &Updater{
		current: strings.TrimSpace(current),
		repo:    repo,
		asset:   defaultUpdateAsset,
		enabled: enabled,
	}
}

// Check returns whether a newer release exists.
// When UPDATE_CHECK=0, skips the network. Dev/empty builds still fetch so the UI can show
// latest tag + publish date, but never report update_available.
func (u *Updater) Check(ctx context.Context) (*UpdateInfo, error) {
	now := time.Now().UTC()
	skipped := !u.enabled || u.current == "" || u.current == "dev"
	if !u.enabled {
		return &UpdateInfo{
			Current:     displayVersion(u.current),
			Latest:      displayVersion(u.current),
			RepoURL:     u.repoURL(),
			ReleasesURL: u.releasesURL(),
			CheckedAt:   now,
			Skipped:     true,
		}, nil
	}

	u.mu.Lock()
	if u.cache != nil && now.Sub(u.cache.CheckedAt) < updateCacheTTL {
		out := *u.cache
		u.mu.Unlock()
		return &out, nil
	}
	u.mu.Unlock()

	info, err := u.fetchLatest(ctx)
	if err != nil {
		// Still surface version + repo link when GitHub is unreachable.
		if skipped {
			return &UpdateInfo{
				Current:     displayVersion(u.current),
				Latest:      displayVersion(u.current),
				RepoURL:     u.repoURL(),
				ReleasesURL: u.releasesURL(),
				CheckedAt:   now,
				Skipped:     true,
			}, nil
		}
		return nil, err
	}
	info.CheckedAt = now
	if skipped {
		info.UpdateAvailable = false
		info.DownloadURL = ""
		info.Skipped = true
		info.Current = displayVersion(u.current)
	}

	u.mu.Lock()
	u.cache = info
	u.mu.Unlock()
	return info, nil
}

func (u *Updater) repoURL() string {
	return "https://github.com/" + u.repo
}

func (u *Updater) releasesURL() string {
	return u.repoURL() + "/releases"
}

func (u *Updater) fetchLatest(ctx context.Context) (*UpdateInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", u.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Instaboard/"+displayVersion(u.current))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("check for updates: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("check for updates: HTTP %d %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var release struct {
		TagName     string `json:"tag_name"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}

	downloadURL := ""
	for _, a := range release.Assets {
		if a.Name == u.asset {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}

	latest := release.TagName
	available := false
	if downloadURL != "" && semver.IsValid(normalizeSemver(latest)) && semver.IsValid(normalizeSemver(u.current)) {
		available = semver.Compare(normalizeSemver(latest), normalizeSemver(u.current)) > 0
	} else if downloadURL != "" && latest != "" && latest != u.current {
		// Non-semver tags: treat unequal tags as an update when we have an asset.
		available = normalizeSemver(latest) != normalizeSemver(u.current)
	}

	notes := strings.TrimSpace(release.Body)
	if len(notes) > 400 {
		notes = notes[:400] + "…"
	}

	var publishedAt *time.Time
	if t, err := time.Parse(time.RFC3339, release.PublishedAt); err == nil {
		utc := t.UTC()
		publishedAt = &utc
	}

	return &UpdateInfo{
		Current:         displayVersion(u.current),
		Latest:          displayVersion(latest),
		UpdateAvailable: available,
		DownloadURL:     downloadURL,
		Notes:           notes,
		RepoURL:         u.repoURL(),
		ReleasesURL:     u.releasesURL(),
		ReleaseURL:      release.HTMLURL,
		PublishedAt:     publishedAt,
	}, nil
}

// Download fetches the latest Mac zip into ~/Downloads and reveals it in Finder on macOS.
func (u *Updater) Download(ctx context.Context) (*UpdateDownloadResult, error) {
	info, err := u.Check(ctx)
	if err != nil {
		return nil, err
	}
	if info.Skipped {
		return nil, fmt.Errorf("update checks are disabled in this build")
	}
	if !info.UpdateAvailable || info.DownloadURL == "" {
		return nil, fmt.Errorf("no update available to download")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	destDir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create Downloads folder: %w", err)
	}
	dest := filepath.Join(destDir, u.asset)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Instaboard/"+displayVersion(u.current))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download update: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download update: HTTP %d", res.StatusCode)
	}

	tmp := dest + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("save update: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}

	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", "-R", dest).Start()
	}
	return &UpdateDownloadResult{Path: dest}, nil
}

func normalizeSemver(v string) string {
	v = strings.TrimSpace(v)
	// Strip dirty/build suffixes that break semver (e.g. v0.1.0-dirty from git describe).
	if i := strings.Index(v, "-dirty"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func displayVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "dev"
	}
	return v
}
