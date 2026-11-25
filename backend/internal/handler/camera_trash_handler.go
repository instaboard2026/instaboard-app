package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"

	"instaboard/internal/middleware"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

// SetCameraTrash wires storage for card files moved to the Trash (shown in the Bin).
func (h *CameraHandler) SetCameraTrash(t *repository.CameraTrashRepo) { h.trash = t }

type cardTrashJSON struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	ByteSize  int64  `json:"byte_size"`
	TrashedAt string `json:"trashed_at"`
	// CardIn is true when the card holding this file is plugged in (Put back / Delete work).
	CardIn   bool   `json:"card_in"`
	ThumbURL string `json:"thumb_url,omitempty"`
	// URL plays / shows the file itself (while the card is in).
	URL string `json:"url,omitempty"`
}

// CardTrash lists card files in the Bin. While a card is in, entries whose file left its Trash
// (emptied or put back in Finder) are dropped.
func (h *CameraHandler) CardTrash(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	items, err := h.trash.List(r.Context(), userID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "could not load card files in the Bin", nil)
		return
	}
	out := make([]cardTrashJSON, 0, len(items))
	for _, it := range items {
		cardIn := service.CameraRootAvailable(it.CardRoot)
		// Older records have no location: look for the file under its own name in the card's Trash.
		if cardIn && it.TrashPath == "" {
			if guess := service.TrashGuess(filepath.Join(it.CardRoot, it.RelPath)); guess != "" {
				if fi, err := os.Stat(guess); err == nil && fi.Mode().IsRegular() && (it.ByteSize == 0 || fi.Size() == it.ByteSize) {
					it.TrashPath = guess
					_ = h.trash.SetTrashPath(r.Context(), userID, it.ID, guess)
				}
			}
		}
		if cardIn && it.TrashPath != "" {
			if _, err := os.Stat(it.TrashPath); err != nil {
				_ = h.trash.Delete(r.Context(), userID, it.ID)
				continue
			}
		}
		j := cardTrashJSON{ID: it.ID, Name: it.Name, Kind: it.Kind, ByteSize: it.ByteSize, TrashedAt: it.TrashedAt, CardIn: cardIn && it.TrashPath != ""}
		if j.CardIn {
			j.ThumbURL = h.media.CardTrashThumbURL(userID, it.ID)
			j.URL = h.media.CardTrashURL(userID, it.ID)
		}
		out = append(out, j)
	}
	utils.WriteSuccess(w, out)
}

// RestoreCardTrash moves a file from the Trash back to where it was on the card.
func (h *CameraHandler) RestoreCardTrash(w http.ResponseWriter, r *http.Request) {
	userID, it, ok := h.cardTrashItem(w, r)
	if !ok {
		return
	}
	dest, err := service.ResolveUnderRoot(it.CardRoot, it.RelPath)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid file location", nil)
		return
	}
	if _, err := os.Stat(dest); err == nil {
		utils.WriteError(w, http.StatusConflict, "a file with that name is already back on the card", nil)
		return
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "could not put the file back", nil)
		return
	}
	if err := os.Rename(it.TrashPath, dest); err != nil {
		slog.Warn("put back card file failed", "from", it.TrashPath, "to", dest, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not put the file back", nil)
		return
	}
	removeAppleDouble(it.TrashPath)
	_ = h.trash.Delete(r.Context(), userID, it.ID)
	// Show it on Manage camera again straight away.
	fi, _ := os.Stat(dest)
	row := repository.CameraIndexRow{RelPath: it.RelPath, Name: it.Name, Kind: it.Kind, ByteSize: it.ByteSize, MtimeNs: it.MtimeNs}
	if fi != nil {
		row.ByteSize, row.MtimeNs = fi.Size(), fi.ModTime().UnixNano()
	}
	if cr, err := h.settings.GetCameraRoot(r.Context(), userID); err == nil && cr.Root == it.CardRoot {
		if err := h.index.InsertBatch(r.Context(), userID, []repository.CameraIndexRow{row}); err != nil {
			slog.Warn("re-index put back file failed", "file", it.RelPath, "err", err)
		}
	}
	utils.WriteSuccess(w, map[string]string{"name": it.Name})
}

// DeleteCardTrash deletes a file from the card's Trash for good.
func (h *CameraHandler) DeleteCardTrash(w http.ResponseWriter, r *http.Request) {
	userID, it, ok := h.cardTrashItem(w, r)
	if !ok {
		return
	}
	if err := os.Remove(it.TrashPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("delete trashed card file failed", "path", it.TrashPath, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not delete the file (card may be locked)", nil)
		return
	}
	removeAppleDouble(it.TrashPath)
	_ = h.trash.Delete(r.Context(), userID, it.ID)
	utils.WriteSuccess(w, map[string]string{"name": it.Name})
}

// removeAppleDouble deletes the hidden "._name" file macOS leaves beside files on exFAT/FAT cards.
func removeAppleDouble(path string) {
	_ = os.Remove(filepath.Join(filepath.Dir(path), "._"+filepath.Base(path)))
}

// CardTrashMedia streams a card file that is in the Bin, so it can be previewed and played.
func (h *CameraHandler) CardTrashMedia(w http.ResponseWriter, r *http.Request) {
	userID, err1 := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	id, err2 := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	q := r.URL.Query()
	if err1 != nil || err2 != nil || !h.media.VerifyCardTrash(userID, id, q.Get("exp"), q.Get("sig")) {
		http.Error(w, "link expired or invalid", http.StatusForbidden)
		return
	}
	it, err := h.trash.Get(r.Context(), userID, id)
	if err != nil || it.TrashPath == "" {
		http.NotFound(w, r)
		return
	}
	serveCardFile(w, r, it.TrashPath)
}

// CardTrashThumb serves the thumbnail of a card file in the Bin (same cache as Manage camera).
func (h *CameraHandler) CardTrashThumb(w http.ResponseWriter, r *http.Request) {
	userID, err1 := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	id, err2 := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	q := r.URL.Query()
	if err1 != nil || err2 != nil || !h.media.VerifyCardTrashThumb(userID, id, q.Get("exp"), q.Get("sig")) {
		http.Error(w, "link expired or invalid", http.StatusForbidden)
		return
	}
	it, err := h.trash.Get(r.Context(), userID, id)
	if err != nil || it.TrashPath == "" {
		http.NotFound(w, r)
		return
	}
	dir := h.downloadDir(r, userID)
	thumb, err := service.EnsureCameraThumbSingleFlight(r.Context(), dir, userID, it.TrashPath, it.RelPath, it.ByteSize, it.MtimeNs)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			http.Error(w, "thumbnail unavailable", http.StatusNotFound)
		}
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	serveFile(w, r, thumb)
}

func (h *CameraHandler) cardTrashItem(w http.ResponseWriter, r *http.Request) (int64, *repository.CardTrashItem, bool) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return 0, nil, false
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid id", nil)
		return 0, nil, false
	}
	it, err := h.trash.Get(r.Context(), userID, id)
	if errors.Is(err, repository.ErrCardTrashNotFound) {
		utils.WriteError(w, http.StatusNotFound, err.Error(), nil)
		return 0, nil, false
	}
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "could not load the file", nil)
		return 0, nil, false
	}
	if it.TrashPath == "" {
		utils.WriteError(w, http.StatusConflict, "Instaboard couldn't tell where this file went in the Trash — use Put Back in Finder", nil)
		return 0, nil, false
	}
	if !service.CameraRootAvailable(it.CardRoot) {
		utils.WriteError(w, http.StatusFailedDependency, "plug the card in first", nil)
		return 0, nil, false
	}
	return userID, it, true
}
