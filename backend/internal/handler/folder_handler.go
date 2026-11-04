package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"instaboard/internal/middleware"
	"instaboard/internal/repository"
	"instaboard/pkg/utils"
)

// FolderHandler handles folders and moving saves between folders and the Dump (authenticated).
type FolderHandler struct {
	folders *repository.FolderRepo
	saves   *repository.SaveRepo
	dump    *DumpHandler
}

// NewFolderHandler creates a new FolderHandler; dump provides the shared save response building.
func NewFolderHandler(folders *repository.FolderRepo, saves *repository.SaveRepo, dump *DumpHandler) *FolderHandler {
	return &FolderHandler{folders: folders, saves: saves, dump: dump}
}

type folderNameRequest struct {
	Name string `json:"name"`
}

type folderOrderRequest struct {
	IDs []int64 `json:"ids"`
}

type moveSaveRequest struct {
	// FolderID nil moves the save back to the Dump.
	FolderID *int64 `json:"folder_id"`
}

type deleteFolderResponse struct {
	DeletedSaves int `json:"deleted_saves"`
}

func userAndID(w http.ResponseWriter, r *http.Request, param string) (userID, id int64, ok bool) {
	userID = middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return 0, 0, false
	}
	if param == "" {
		return userID, 0, true
	}
	id, err := strconv.ParseInt(chi.URLParam(r, param), 10, 64)
	if err != nil || id <= 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid id", nil)
		return 0, 0, false
	}
	return userID, id, true
}

// writeFolderError maps repository errors to HTTP responses; returns false if err was nil.
func writeFolderError(w http.ResponseWriter, err error, action string, userID int64) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, repository.ErrFolderNotFound):
		utils.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, repository.ErrSaveNotFound):
		utils.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, repository.ErrFolderNameTaken):
		utils.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, repository.ErrFolderNameInvalid), errors.Is(err, repository.ErrTooManyFolders), errors.Is(err, repository.ErrFolderOrder):
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		slog.Error(action+" failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, action+" failed", nil)
	}
	return true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return false
	}
	return true
}

// List returns the user's folders in sidebar order with save counts.
func (h *FolderHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := userAndID(w, r, "")
	if !ok {
		return
	}
	folders, err := h.folders.List(r.Context(), userID)
	if writeFolderError(w, err, "list folders", userID) {
		return
	}
	utils.WriteSuccess(w, folders)
}

// Create adds a folder; an empty or missing name becomes the next free "Folder N".
func (h *FolderHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := userAndID(w, r, "")
	if !ok {
		return
	}
	var req folderNameRequest
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	folder, err := h.folders.Create(r.Context(), userID, req.Name)
	if writeFolderError(w, err, "create folder", userID) {
		return
	}
	utils.WriteJSON(w, http.StatusCreated, utils.APIResponse{Status: "SUCCESS", Data: folder})
}

// Rename changes a folder's name.
func (h *FolderHandler) Rename(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	var req folderNameRequest
	if !decode(w, r, &req) {
		return
	}
	folder, err := h.folders.Rename(r.Context(), userID, id, req.Name)
	if writeFolderError(w, err, "rename folder", userID) {
		return
	}
	utils.WriteSuccess(w, folder)
}

// Delete removes a folder and the saves in it (downloaded files are kept on disk).
func (h *FolderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	n, err := h.folders.Delete(r.Context(), userID, id)
	if writeFolderError(w, err, "delete folder", userID) {
		return
	}
	utils.WriteSuccess(w, deleteFolderResponse{DeletedSaves: n})
}

// Reorder sets the sidebar order of the user's folders.
func (h *FolderHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := userAndID(w, r, "")
	if !ok {
		return
	}
	var req folderOrderRequest
	if !decode(w, r, &req) {
		return
	}
	if writeFolderError(w, h.folders.Reorder(r.Context(), userID, req.IDs), "reorder folders", userID) {
		return
	}
	folders, err := h.folders.List(r.Context(), userID)
	if writeFolderError(w, err, "list folders", userID) {
		return
	}
	utils.WriteSuccess(w, folders)
}

// ListSaves returns the saves in a folder (same shape as GET /api/dump).
func (h *FolderHandler) ListSaves(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	if _, err := h.folders.Get(r.Context(), userID, id); writeFolderError(w, err, "load folder", userID) {
		return
	}
	saves, err := h.saves.ListByFolder(r.Context(), userID, id)
	if writeFolderError(w, err, "list folder saves", userID) {
		return
	}
	utils.WriteSuccess(w, h.dump.saveViews(r, userID, saves))
}

// AddSave saves a pasted link straight into a folder.
func (h *FolderHandler) AddSave(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	h.dump.addLink(w, r, userID, &id)
}

// MoveSave moves a save into a folder, or back to the Dump when folder_id is null.
func (h *FolderHandler) MoveSave(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	var req moveSaveRequest
	if !decode(w, r, &req) {
		return
	}
	save, err := h.saves.Move(r.Context(), userID, uint64(id), req.FolderID)
	if writeFolderError(w, err, "move save", userID) {
		return
	}
	dir, _ := h.dump.downloadDir(r, userID)
	utils.WriteSuccess(w, h.dump.saveView(userID, dir, *save))
}
