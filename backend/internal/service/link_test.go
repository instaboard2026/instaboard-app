package service

import "testing"

func TestNormalizeLink(t *testing.T) {
	tests := []struct {
		in, want, source string
		wantErr          error
	}{
		{"https://www.instagram.com/reel/C9xK2pLm4Qa/?igsh=abc123", "https://instagram.com/reel/C9xK2pLm4Qa", SourceInstagram, nil},
		{"instagram.com/p/DB0hR4nYc8k", "https://instagram.com/p/DB0hR4nYc8k", SourceInstagram, nil},
		{"https://pin.it/4bZq9KxT", "https://pin.it/4bZq9KxT", SourcePinterest, nil},
		{"https://in.pinterest.com/pin/123456/?utm_source=x", "https://in.pinterest.com/pin/123456", SourcePinterest, nil},
		{"https://youtube.com/shorts/q7Lm2Vx9aB0?si=xyz", "https://youtube.com/shorts/q7Lm2Vx9aB0", SourceYouTube, nil},
		{"https://m.youtube.com/watch?v=abc&t=10s&si=1", "https://youtube.com/watch?v=abc", SourceYouTube, nil},
		{"https://youtu.be/abc?si=1", "https://youtu.be/abc", SourceYouTube, nil},
		{"https://youtube.com/watch", "", "", ErrLinkInvalid},
		{"https://instagram.com/", "", "", ErrLinkInvalid},
		{"https://example.com/video", "", "", ErrLinkUnsupported},
		{"ftp://instagram.com/reel/x", "", "", ErrLinkInvalid},
		{"   ", "", "", ErrLinkInvalid},
	}
	for _, tt := range tests {
		got, source, err := NormalizeLink(tt.in)
		if err != tt.wantErr {
			t.Errorf("NormalizeLink(%q) err = %v, want %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want || source != tt.source {
			t.Errorf("NormalizeLink(%q) = (%q, %q), want (%q, %q)", tt.in, got, source, tt.want, tt.source)
		}
	}
}
