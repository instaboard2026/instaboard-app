package handler

import (
	"net/http/httptest"
	"testing"
)

func TestIsLocalRequest(t *testing.T) {
	cases := []struct {
		remote, host string
		want         bool
	}{
		{"127.0.0.1:52000", "localhost:8788", true},
		{"127.0.0.1:52000", "127.0.0.1:8788", true},
		{"[::1]:52000", "[::1]:8788", true},
		{"192.168.1.20:52000", "localhost:8788", false},     // another device on the network
		{"127.0.0.1:52000", "evil.example.com:8788", false}, // DNS rebinding
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/auth/local", nil)
		r.RemoteAddr, r.Host = c.remote, c.host
		if got := isLocalRequest(r); got != c.want {
			t.Errorf("isLocalRequest(remote=%s, host=%s) = %v, want %v", c.remote, c.host, got, c.want)
		}
	}
}
