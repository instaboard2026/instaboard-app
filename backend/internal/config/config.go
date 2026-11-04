package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	Host               string
	Port               int
	DBPath             string
	CORSHost           string
	BackendPublicURL   string
	JWTSecret          string
	DownloadDir        string
	GalleryDLPath      string
	CookiesFromBrowser string
	YTDLModule         string
}

// Load reads configuration from environment variables; every value has a working default.
// Dev default: API on :8788 with Angular at :5200. Packaged builds (embedded UI) listen on :4200
// instead — see main.go — so users open http://localhost:4200/home.
func Load() *Config {
	port, err := strconv.Atoi(getEnv("PORT", "8788"))
	if err != nil {
		port = 8788
	}
	corsHost := getEnv("CORS_ORIGIN", "http://localhost:5200")

	backendURL := strings.TrimSuffix(getEnv("BACKEND_PUBLIC_URL", fmt.Sprintf("http://localhost:%d", port)), "/")

	return &Config{
		// Loopback only: Instaboard is a local app and its API must not be reachable from the network.
		Host:             getEnv("HOST", "127.0.0.1"),
		Port:             port,
		DBPath:           expandHome(getEnv("DB_PATH", "~/.instaboard/instaboard.db")),
		CORSHost:         corsHost,
		BackendPublicURL: backendURL,
		JWTSecret:        getEnv("JWT_SECRET", ""),
		DownloadDir:      expandHome(getEnv("DOWNLOAD_DIR", "~/Movies/Instaboard")),
		GalleryDLPath:    getEnv("GALLERY_DL_PATH", "gallery-dl"),
		// Browser whose Instagram session gallery-dl may use ("chrome", "safari", ...). Set to "none" to stay logged out.
		CookiesFromBrowser: getEnv("COOKIES_FROM_BROWSER", "chrome"),
		YTDLModule:         getEnv("YTDL_MODULE", ""),
	}
}

// EnsureJWTSecret fills JWTSecret when JWT_SECRET is unset, using a random key kept next to the database
// (created on first run), so a downloaded app needs no configuration and sessions survive restarts.
func (c *Config) EnsureJWTSecret() error {
	if c.JWTSecret != "" {
		return nil
	}
	path := filepath.Join(filepath.Dir(c.DBPath), "session.key")
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		c.JWTSecret = strings.TrimSpace(string(b))
		return nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("generate session key: %w", err)
	}
	secret := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return fmt.Errorf("write session key: %w", err)
	}
	c.JWTSecret = secret
	return nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
