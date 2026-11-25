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
	case ".mp4", ".webm", ".mov", ".m4v", ".avi", ".mkv", ".mts", ".m2ts", ".3gp":
		return "video"
	default:
		return "image"
	}
}

func (m *MediaSigner) splitSignature(userID int64, projectID uint64, file string, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	fmt.Fprintf(mac, "media/split/%d/%d/%s/%d", userID, projectID, file, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// SplitURL returns a signed URL for a file under a split project (master.mp4 or clips/N.mp4).
func (m *MediaSigner) SplitURL(userID int64, projectID uint64, file string) MediaItem {
	exp := time.Now().Add(m.ttl).Unix()
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", m.splitSignature(userID, projectID, file, exp))
	escaped := strings.ReplaceAll(url.PathEscape(file), "%2F", "/")
	return MediaItem{
		URL:  fmt.Sprintf("%s/media/split/%d/%d/%s?%s", m.baseURL, userID, projectID, escaped, q.Encode()),
		Type: MediaType(file),
	}
}

// VerifySplit checks a split media URL's signature and expiry.
func (m *MediaSigner) VerifySplit(userID int64, projectID uint64, file, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.splitSignature(userID, projectID, file, exp)))
}

func (m *MediaSigner) cameraSignature(userID int64, file string, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	fmt.Fprintf(mac, "media/camera/%d/%s/%d", userID, file, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// CameraURL returns a signed URL for a file under the user's camera root (rel path, slash-separated).
func (m *MediaSigner) CameraURL(userID int64, relPath string) MediaItem {
	exp := time.Now().Add(m.ttl).Unix()
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", m.cameraSignature(userID, relPath, exp))
	escaped := strings.ReplaceAll(url.PathEscape(relPath), "%2F", "/")
	return MediaItem{
		URL:  fmt.Sprintf("%s/media/camera/%d/%s?%s", m.baseURL, userID, escaped, q.Encode()),
		Type: MediaType(relPath),
	}
}

// VerifyCamera checks a camera media URL's signature and expiry.
func (m *MediaSigner) VerifyCamera(userID int64, file, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.cameraSignature(userID, file, exp)))
}

func (m *MediaSigner) localMediaSignature(userID int64, itemID uint64, file string, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	fmt.Fprintf(mac, "media/local-media/%d/%d/%s/%d", userID, itemID, file, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// LocalMediaURL returns a signed URL for a Local media library file.
func (m *MediaSigner) LocalMediaURL(userID int64, itemID uint64, file string) MediaItem {
	exp := time.Now().Add(m.ttl).Unix()
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", m.localMediaSignature(userID, itemID, file, exp))
	return MediaItem{
		URL:  fmt.Sprintf("%s/media/local-media/%d/%d/%s?%s", m.baseURL, userID, itemID, url.PathEscape(file), q.Encode()),
		Type: MediaType(file),
	}
}

// VerifyLocalMedia checks a Local media URL's signature and expiry.
func (m *MediaSigner) VerifyLocalMedia(userID int64, itemID uint64, file, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.localMediaSignature(userID, itemID, file, exp)))
}

func (m *MediaSigner) localMediaThumbSignature(userID int64, itemID uint64, file string, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	fmt.Fprintf(mac, "media/local-media-thumb/%d/%d/%s/%d", userID, itemID, file, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// LocalMediaThumbURL returns a signed URL for a JPEG thumbnail of a Local media file.
func (m *MediaSigner) LocalMediaThumbURL(userID int64, itemID uint64, file string) string {
	exp := time.Now().Add(m.ttl).Unix()
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", m.localMediaThumbSignature(userID, itemID, file, exp))
	return fmt.Sprintf("%s/media/local-media-thumb/%d/%d/%s?%s", m.baseURL, userID, itemID, url.PathEscape(file), q.Encode())
}

// VerifyLocalMediaThumb checks a Local media thumbnail URL's signature and expiry.
func (m *MediaSigner) VerifyLocalMediaThumb(userID int64, itemID uint64, file, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.localMediaThumbSignature(userID, itemID, file, exp)))
}

func (m *MediaSigner) cameraThumbSignature(userID int64, file string, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	fmt.Fprintf(mac, "media/camera-thumb/%d/%s/%d", userID, file, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// CameraThumbURL returns a signed URL for a JPEG thumbnail of a camera file.
func (m *MediaSigner) CameraThumbURL(userID int64, relPath string) string {
	exp := time.Now().Add(m.ttl).Unix()
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", m.cameraThumbSignature(userID, relPath, exp))
	escaped := strings.ReplaceAll(url.PathEscape(relPath), "%2F", "/")
	return fmt.Sprintf("%s/media/camera-thumb/%d/%s?%s", m.baseURL, userID, escaped, q.Encode())
}

// VerifyCameraThumb checks a camera thumbnail URL's signature and expiry.
func (m *MediaSigner) VerifyCameraThumb(userID int64, file, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.cameraThumbSignature(userID, file, exp)))
}

func (m *MediaSigner) cardTrashSignature(userID, id, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	fmt.Fprintf(mac, "media/card-trash-thumb/%d/%d/%d", userID, id, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// CardTrashThumbURL returns a signed URL for the thumbnail of a camera file that is in the Bin.
func (m *MediaSigner) CardTrashThumbURL(userID, id int64) string {
	exp := time.Now().Add(m.ttl).Unix()
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", m.cardTrashSignature(userID, id, exp))
	return fmt.Sprintf("%s/media/card-trash-thumb/%d/%d?%s", m.baseURL, userID, id, q.Encode())
}

// VerifyCardTrashThumb checks a Bin card-file thumbnail URL.
func (m *MediaSigner) VerifyCardTrashThumb(userID, id int64, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.cardTrashSignature(userID, id, exp)))
}

func (m *MediaSigner) cardTrashMediaSignature(userID, id, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	fmt.Fprintf(mac, "media/card-trash/%d/%d/%d", userID, id, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// CardTrashURL returns a signed URL that streams a card file that is in the Bin.
func (m *MediaSigner) CardTrashURL(userID, id int64) string {
	exp := time.Now().Add(m.ttl).Unix()
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", m.cardTrashMediaSignature(userID, id, exp))
	return fmt.Sprintf("%s/media/card-trash/%d/%d?%s", m.baseURL, userID, id, q.Encode())
}

// VerifyCardTrash checks a Bin card-file media URL.
func (m *MediaSigner) VerifyCardTrash(userID, id int64, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.cardTrashMediaSignature(userID, id, exp)))
}
