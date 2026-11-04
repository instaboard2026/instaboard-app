package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MediaItem is one downloaded file the browser can show, with a signed URL (img/video tags can't send auth headers).
type MediaItem struct {
	URL  string `json:"url"`
	Type string `json:"type"` // "video" | "image"
}

// MediaSigner creates and checks expiring HMAC-signed URLs for downloaded files.
type MediaSigner struct {
	secret  []byte
	baseURL string
	ttl     time.Duration
}

// NewMediaSigner creates a signer; baseURL is the backend's public URL (e.g. http://localhost:8788).
func NewMediaSigner(secret, baseURL string, ttl time.Duration) *MediaSigner {
	return &MediaSigner{secret: []byte(secret), baseURL: strings.TrimSuffix(baseURL, "/"), ttl: ttl}
}

func (m *MediaSigner) signature(userID int64, saveID uint64, file string, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	fmt.Fprintf(mac, "media/%d/%d/%s/%d", userID, saveID, file, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// Items returns signed URLs for a save's downloaded files.
func (m *MediaSigner) Items(userID int64, saveID uint64, files []string) []MediaItem {
	exp := time.Now().Add(m.ttl).Unix()
	items := make([]MediaItem, 0, len(files))
	for _, f := range files {
		q := url.Values{}
		q.Set("exp", strconv.FormatInt(exp, 10))
		q.Set("sig", m.signature(userID, saveID, f, exp))
		items = append(items, MediaItem{
			URL:  fmt.Sprintf("%s/media/%d/%d/%s?%s", m.baseURL, userID, saveID, url.PathEscape(f), q.Encode()),
			Type: MediaType(f),
		})
	}
	return items
}

// Verify checks a media URL's signature and expiry.
func (m *MediaSigner) Verify(userID int64, saveID uint64, file, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.signature(userID, saveID, file, exp)))
}

// MediaType classifies a file by extension.
func MediaType(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".mp4", ".webm", ".mov", ".m4v":
		return "video"
	default:
		return "image"
	}
}
