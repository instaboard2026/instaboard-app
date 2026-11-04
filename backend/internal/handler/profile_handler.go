package handler

import (
	"log/slog"
	"net/http"
	"time"

	"instaboard/internal/middleware"
	"instaboard/internal/repository"
	"instaboard/pkg/utils"
)

// ProfileResponse is the user profile returned by GET /api/profile.
type ProfileResponse struct {
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	MemberSince string `json:"member_since,omitempty"`
}

// ProfileHandler handles GET /api/profile (authenticated).
type ProfileHandler struct {
	userRepo *repository.UserRepo
}

// NewProfileHandler creates a new ProfileHandler.
func NewProfileHandler(userRepo *repository.UserRepo) *ProfileHandler {
	return &ProfileHandler{userRepo: userRepo}
}

// GetProfile returns the current user's profile. Requires JWT middleware.
func (h *ProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil {
		slog.Error("profile find by id failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to fetch profile", nil)
		return
	}
	if user == nil {
		utils.WriteError(w, http.StatusNotFound, "user not found", nil)
		return
	}
	resp := ProfileResponse{Name: user.Name.String, Email: user.PrimaryEmail.String}
	if user.CreatedAt.Valid {
		resp.MemberSince = user.CreatedAt.Time.Format(time.RFC3339)
	}
	utils.WriteSuccess(w, resp)
}
