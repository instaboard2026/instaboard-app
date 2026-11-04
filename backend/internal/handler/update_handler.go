package handler

import (
	"log/slog"
	"net/http"

	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

// UpdateHandler serves update check and download endpoints.
type UpdateHandler struct {
	updater *service.Updater
}

// NewUpdateHandler creates an UpdateHandler.
func NewUpdateHandler(updater *service.Updater) *UpdateHandler {
	return &UpdateHandler{updater: updater}
}

// Check handles GET /api/update.
func (h *UpdateHandler) Check(w http.ResponseWriter, r *http.Request) {
	info, err := h.updater.Check(r.Context())
	if err != nil {
		slog.Warn("update check failed", "err", err)
		utils.WriteError(w, http.StatusBadGateway, "could not check for updates", nil)
		return
	}
	utils.WriteSuccess(w, info)
}

// Download handles POST /api/update/download.
func (h *UpdateHandler) Download(w http.ResponseWriter, r *http.Request) {
	result, err := h.updater.Download(r.Context())
	if err != nil {
		slog.Warn("update download failed", "err", err)
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	utils.WriteSuccess(w, result)
}
