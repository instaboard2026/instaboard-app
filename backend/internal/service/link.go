package service

import (
	"errors"
	"net/url"
	"strings"
)

// Link sources supported by Instaboard.
const (
	SourceInstagram = "instagram"
	SourcePinterest = "pinterest"
	SourceYouTube   = "youtube"
)

const maxLinkLength = 2048

var (
	ErrLinkInvalid     = errors.New("not a valid link")
	ErrLinkUnsupported = errors.New("only Instagram, Pinterest and YouTube links are supported")
)

// NormalizeLink validates a pasted link, detects its source and returns a canonical URL
// (https, no www/m prefix, no tracking params or fragment) so the same post saves only once.
func NormalizeLink(raw string) (normalized, source string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxLinkLength {
		return "", "", ErrLinkInvalid
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", ErrLinkInvalid
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")

	path := strings.TrimRight(u.EscapedPath(), "/")
	query := ""

	switch {
	case host == "instagram.com":
		source = SourceInstagram
	case host == "pin.it" || isPinterestHost(host):
		source = SourcePinterest
	case host == "youtu.be" || host == "youtube.com" || host == "music.youtube.com":
		source = SourceYouTube
		if path == "/watch" {
			v := u.Query().Get("v")
			if v == "" {
				return "", "", ErrLinkInvalid
			}
			query = "?v=" + url.QueryEscape(v)
		}
	default:
		return "", "", ErrLinkUnsupported
	}

	if path == "" {
		return "", "", ErrLinkInvalid
	}
	return "https://" + host + path + query, source, nil
}

// isPinterestHost matches pinterest.com and country domains like in.pinterest.com or pinterest.co.uk.
func isPinterestHost(host string) bool {
	for _, label := range strings.Split(host, ".") {
		if label == "pinterest" {
			return true
		}
	}
	return false
}
