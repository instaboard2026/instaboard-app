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
	// afterApply is called after a successful Apply (replace binary); typically triggers restart.
	afterApply func()
}

// NewUpdateHandler creates an UpdateHandler. afterApply may be nil.
func NewUpdateHandler(updater *service.Updater, afterApply func()) *UpdateHandler {
	return &UpdateHandler{updater: updater, afterApply: afterApply}
}

// Check handles GET /api/update. Pass ?refresh=1 to bypass the cache.
func (h *UpdateHandler) Check(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	info, err := h.updater.Check(r.Context(), refresh)
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

// Apply handles POST /api/update/apply — download, replace the binary, then restart.
// Pass ?force=1 to reinstall the latest release even when already on that version.
func (h *UpdateHandler) Apply(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	result, err := h.updater.Apply(r.Context(), force)
	if err != nil {
		slog.Warn("update apply failed", "err", err, "force", force)
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	utils.WriteSuccess(w, result)
	if h.afterApply != nil {
		h.afterApply()
	}
}
