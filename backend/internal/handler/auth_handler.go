package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"instaboard/internal/config"
	"instaboard/internal/service"
)

const jwtExpiry = 30 * 24 * time.Hour

// AuthHandler signs in the local app's single user (no external sign-in).
type AuthHandler struct {
	cfg         *config.Config
	authSvc     *service.AuthService
	accountName func() string
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(cfg *config.Config, authSvc *service.AuthService) *AuthHandler {
	return &AuthHandler{
		cfg:         cfg,
		authSvc:     authSvc,
		accountName: sync.OnceValue(service.AccountName),
	}
}

// Local handles POST /auth/local: returns a session token for the local user, named after the computer account.
// Only requests made on this computer are answered.
func (h *AuthHandler) Local(w http.ResponseWriter, r *http.Request) {
	if !isLocalRequest(r) {
		http.Error(w, "Instaboard only accepts requests from this computer", http.StatusForbidden)
		return
	}
	user, err := h.authSvc.LocalUser(r.Context(), h.accountName())
	if err != nil {
		slog.Error("local user lookup failed", "err", err)
		http.Error(w, "Could not open your Instaboard", http.StatusInternalServerError)
		return
	}
	token, err := h.signJWT(user.ID, user.PrimaryEmail.String, user.Name.String)
	if err != nil {
		slog.Error("jwt sign failed", "err", err)
		http.Error(w, "Could not open your Instaboard", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// isLocalRequest reports whether the request comes from this machine and names it by a loopback host,
// which also stops other websites from reaching the API through DNS rebinding.
func isLocalRequest(r *http.Request) bool {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *AuthHandler) signJWT(userID int64, email, name string) (string, error) {
	if h.cfg.JWTSecret == "" {
		return "", fmt.Errorf("JWT secret not set")
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": strconv.FormatInt(userID, 10),
		"iat": now.Unix(),
		"exp": now.Add(jwtExpiry).Unix(),
	}
	if email != "" {
		claims["email"] = email
	}
	if name != "" {
		claims["name"] = name
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.cfg.JWTSecret))
}
