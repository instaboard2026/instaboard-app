package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"instaboard/internal/middleware"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

const (
	maxUploadImageBytes = 25 << 20 // 25 MiB
	maxUploadFiles      = 20
	maxMultipartMemory  = 32 << 20
)

// UploadToDump handles POST /api/dump/upload — dropped images become Dump cards.
func (h *DumpHandler) UploadToDump(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	h.uploadImages(w, r, userID, nil)
}

// UploadToFolder handles POST /api/folders/{id}/saves/upload.
func (h *FolderHandler) UploadToFolder(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	h.dump.uploadImages(w, r, userID, &id)
}

func (h *DumpHandler) uploadImages(w http.ResponseWriter, r *http.Request, userID int64, folderID *int64) {
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "could not read uploaded files", nil)
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		// Also accept a single field named "file" without MultiFile.
		if f, hdr, err := r.FormFile("file"); err == nil {
			defer f.Close()
			files = []*multipart.FileHeader{hdr}
		}
	}
	if len(files) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "drop one or more image files", nil)
		return
	}
	if len(files) > maxUploadFiles {
		utils.WriteError(w, http.StatusBadRequest, fmt.Sprintf("too many files (max %d)", maxUploadFiles), nil)
		return
	}

	dir, _ := h.downloadDir(r, userID)
	out := make([]saveResponse, 0, len(files))
	for _, hdr := range files {
		ext, err := imageExt(hdr)
		if err != nil {
			utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if hdr.Size > maxUploadImageBytes {
			utils.WriteError(w, http.StatusBadRequest, "image is too large (max 25 MB)", nil)
			return
		}

		url, err := newLocalURL()
		if err != nil {
			slog.Error("local url failed", "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "failed to save image", nil)
			return
		}
		save, err := h.repo.Insert(r.Context(), userID, url, service.SourceLocal, "drop", folderID)
		switch {
		case errors.Is(err, repository.ErrFolderNotFound):
			utils.WriteError(w, http.StatusNotFound, "folder not found", nil)
			return
		case err != nil:
			slog.Error("insert local save failed", "user_id", userID, "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "failed to save image", nil)
			return
		}

		src, err := hdr.Open()
		if err != nil {
			_ = h.repo.Delete(r.Context(), userID, save.ID)
			utils.WriteError(w, http.StatusBadRequest, "could not read uploaded file", nil)
			return
		}
		limited := io.LimitReader(src, maxUploadImageBytes+1)
		state, storeErr := h.downloader.StoreLocalImage(dir, save.ID, ext, limited)
		src.Close()
		if storeErr != nil {
			h.removeDownloadFiles(r, userID, save.ID)
			_ = h.repo.Delete(r.Context(), userID, save.ID)
			slog.Error("store local image failed", "user_id", userID, "save_id", save.ID, "err", storeErr)
			utils.WriteError(w, http.StatusInternalServerError, "failed to store image", nil)
			return
		}
		out = append(out, saveResponse{Save: *save, Download: h.withMedia(userID, dir, save.ID, state)})
	}

	utils.WriteJSON(w, http.StatusCreated, utils.APIResponse{Status: "SUCCESS", Data: out})
}

func newLocalURL() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "local://" + hex.EncodeToString(b), nil
}

func imageExt(hdr *multipart.FileHeader) (string, error) {
	name := strings.ToLower(filepath.Base(hdr.Filename))
	ct := strings.ToLower(strings.TrimSpace(hdr.Header.Get("Content-Type")))
	if ct != "" {
		if media, _, err := mime.ParseMediaType(ct); err == nil {
			ct = media
		}
	}
	switch ct {
	case "image/png":
		return ".png", nil
	case "image/jpeg", "image/jpg":
		return ".jpg", nil
	case "image/webp":
		return ".webp", nil
	case "image/gif":
		return ".gif", nil
	}
	switch filepath.Ext(name) {
	case ".png":
		return ".png", nil
	case ".jpg", ".jpeg":
		return ".jpg", nil
	case ".webp":
		return ".webp", nil
	case ".gif":
		return ".gif", nil
	}
	return "", fmt.Errorf("only PNG, JPEG, WebP and GIF images are supported")
}
