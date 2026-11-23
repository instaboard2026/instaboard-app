package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"instaboard/internal/middleware"
	"instaboard/internal/models"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

// LocalMediaHandler serves Local media folders and copied files.
type LocalMediaHandler struct {
	repo       *repository.LocalMediaRepo
	saves      *repository.SaveRepo
	settings   *repository.SettingsRepo
	media      *service.MediaSigner
	downloader *service.Downloader
	jobs       *service.CopyJobStore
	defaultDir string
	copyMu     sync.Map // userID -> *sync.Mutex
}

// NewLocalMediaHandler creates a LocalMediaHandler.
func NewLocalMediaHandler(
	repo *repository.LocalMediaRepo,
	saves *repository.SaveRepo,
	settings *repository.SettingsRepo,
	media *service.MediaSigner,
	downloader *service.Downloader,
	jobs *service.CopyJobStore,
	defaultDir string,
) *LocalMediaHandler {
	return &LocalMediaHandler{
		repo: repo, saves: saves, settings: settings, media: media,
		downloader: downloader, jobs: jobs, defaultDir: defaultDir,
	}
}

func (h *LocalMediaHandler) downloadDir(r *http.Request, userID int64) string {
	custom, err := h.settings.DownloadDir(r.Context(), userID)
	if err != nil {
		slog.Error("load download dir failed", "user_id", userID, "err", err)
	}
	if custom == "" {
		return h.defaultDir
	}
	return custom
}

func (h *LocalMediaHandler) userDir(downloadDir string, userID int64) string {
	return filepath.Join(downloadDir, "local-media", strconv.FormatInt(userID, 10))
}

func (h *LocalMediaHandler) itemPath(downloadDir string, userID int64, storedName string) string {
	base := filepath.Base(storedName)
	if base != storedName || strings.Contains(base, "..") {
		return ""
	}
	return filepath.Join(h.userDir(downloadDir, userID), base)
}

func (h *LocalMediaHandler) copyLock(userID int64) *sync.Mutex {
	v, _ := h.copyMu.LoadOrStore(userID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

type localMediaItemJSON struct {
	models.LocalMedia
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url"`
	Type     string `json:"type"`
}

func (h *LocalMediaHandler) itemView(userID int64, m models.LocalMedia) localMediaItemJSON {
	signed := h.media.LocalMediaURL(userID, m.ID, m.StoredName)
	return localMediaItemJSON{
		LocalMedia: m,
		URL:        signed.URL,
		ThumbURL:   h.media.LocalMediaThumbURL(userID, m.ID, m.StoredName),
		Type:       signed.Type,
	}
}

// ListFolders returns Local media folders.
func (h *LocalMediaHandler) ListFolders(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	list, err := h.repo.ListFolders(r.Context(), userID)
	if err != nil {
		slog.Error("list local media folders failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load folders", nil)
		return
	}
	utils.WriteSuccess(w, list)
}

// CreateFolder creates a Local media folder.
func (h *LocalMediaHandler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	folder, err := h.repo.CreateFolder(r.Context(), userID, body.Name)
	switch {
	case errors.Is(err, repository.ErrFolderNameInvalid):
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, repository.ErrLocalMediaFolderNameTaken):
		utils.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, repository.ErrTooManyLocalMediaFolders):
		utils.WriteError(w, http.StatusUnprocessableEntity, err.Error(), nil)
	case err != nil:
		slog.Error("create local media folder failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not create folder", nil)
	default:
		utils.WriteSuccess(w, folder)
	}
}

// RenameFolder renames a Local media folder.
func (h *LocalMediaHandler) RenameFolder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	folder, err := h.repo.RenameFolder(r.Context(), userID, id, body.Name)
	switch {
	case errors.Is(err, repository.ErrFolderNameInvalid):
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, repository.ErrLocalMediaFolderNameTaken):
		utils.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, repository.ErrLocalMediaFolderNotFound):
		utils.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case err != nil:
		slog.Error("rename local media folder failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not rename folder", nil)
	default:
		utils.WriteSuccess(w, folder)
	}
}

// ReorderFolders sets Local media folder order.
func (h *LocalMediaHandler) ReorderFolders(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	err := h.repo.ReorderFolders(r.Context(), userID, body.IDs)
	switch {
	case errors.Is(err, repository.ErrLocalMediaFolderOrder):
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case err != nil:
		slog.Error("reorder local media folders failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not reorder folders", nil)
	default:
		utils.WriteSuccess(w, map[string]string{"ok": "true"})
	}
}

// DeleteFolder deletes a folder and its files on disk.
func (h *LocalMediaHandler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}
	items, err := h.repo.DeleteFolder(r.Context(), userID, id)
	if errors.Is(err, repository.ErrLocalMediaFolderNotFound) {
		utils.WriteError(w, http.StatusNotFound, err.Error(), nil)
		return
	}
	if err != nil {
		slog.Error("delete local media folder failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not delete folder", nil)
		return
	}
	dir := h.downloadDir(r, userID)
	for _, item := range items {
		path := h.itemPath(dir, userID, item.StoredName)
		if path != "" {
			_ = os.Remove(path)
		}
	}
	utils.WriteSuccess(w, map[string]int{"deleted_items": len(items)})
}

// ImportFromSave moves a Dump/Home card's downloaded files into a Local media folder, then deletes the save.
func (h *LocalMediaHandler) ImportFromSave(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	folderID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || folderID <= 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}
	var body struct {
		SaveID uint64 `json:"save_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SaveID == 0 {
		utils.WriteError(w, http.StatusBadRequest, "save_id required", nil)
		return
	}
	if _, err := h.repo.GetFolder(r.Context(), userID, folderID); err != nil {
		if errors.Is(err, repository.ErrLocalMediaFolderNotFound) {
			utils.WriteError(w, http.StatusNotFound, "folder not found", nil)
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "could not load folder", nil)
		return
	}
	save, err := h.saves.Get(r.Context(), userID, body.SaveID)
	if errors.Is(err, repository.ErrSaveNotFound) {
		utils.WriteError(w, http.StatusNotFound, "save not found", nil)
		return
	}
	if err != nil {
		slog.Error("get save for local media import failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load save", nil)
		return
	}

	dir := h.downloadDir(r, userID)
	files := h.downloader.MediaFiles(dir, save.ID)
	if len(files) == 0 {
		utils.WriteError(w, http.StatusFailedDependency, "download this item first, then move it to Local media", nil)
		return
	}

	userDir := h.userDir(dir, userID)
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "could not create library folder", nil)
		return
	}

	imported := make([]localMediaItemJSON, 0, len(files))
	for i, file := range files {
		src := h.downloader.MediaPath(dir, save.ID, file)
		fi, err := os.Stat(src)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(file))
		kind := service.MediaType(file)
		if kind != "image" && kind != "video" {
			continue
		}
		original := fmt.Sprintf("%d-%s", save.ID, filepath.Base(file))
		if len(files) == 1 && strings.HasPrefix(file, strconv.FormatUint(save.ID, 10)+".") {
			original = filepath.Base(file)
		}
		item, err := h.repo.InsertItemPlaceholder(r.Context(), repository.InsertItemParams{
			UserID: userID, FolderID: folderID, OriginalName: original, Kind: kind, ByteSize: fi.Size(),
		}, ext)
		if errors.Is(err, repository.ErrLocalMediaNameTaken) {
			original = fmt.Sprintf("%d-%d-%s", save.ID, i+1, filepath.Base(file))
			item, err = h.repo.InsertItemPlaceholder(r.Context(), repository.InsertItemParams{
				UserID: userID, FolderID: folderID, OriginalName: original, Kind: kind, ByteSize: fi.Size(),
			}, ext)
		}
		if err != nil {
			slog.Error("import save into local media failed", "save", save.ID, "file", file, "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "could not move into Local media", nil)
			return
		}
		dest := h.itemPath(dir, userID, item.StoredName)
		tmp := dest + ".tmp"
		if err := copyFileAtomic(src, tmp, dest); err != nil {
			_, _ = h.repo.DeleteItem(r.Context(), userID, item.ID)
			slog.Error("copy save media into local media failed", "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "could not move file into Local media", nil)
			return
		}
		imported = append(imported, h.itemView(userID, *item))
	}
	if len(imported) == 0 {
		utils.WriteError(w, http.StatusFailedDependency, "no movable files on this card", nil)
		return
	}

	_ = h.downloader.RemoveFiles(dir, save.ID)
	if err := h.saves.Delete(r.Context(), userID, save.ID); err != nil {
		slog.Warn("imported to local media but save delete failed", "save", save.ID, "err", err)
	}

	utils.WriteJSON(w, http.StatusCreated, utils.APIResponse{Status: "SUCCESS", Data: map[string]any{
		"imported": len(imported),
		"items":    imported,
	}})
}

// MoveItems moves Local media files into the folder identified by the URL id (DB move only).
func (h *LocalMediaHandler) MoveItems(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	toFolderID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || toFolderID <= 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}
	var body struct {
		IDs []uint64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "ids required", nil)
		return
	}
	moved, err := h.repo.MoveItems(r.Context(), userID, toFolderID, body.IDs)
	if errors.Is(err, repository.ErrLocalMediaFolderNotFound) {
		utils.WriteError(w, http.StatusNotFound, "folder not found", nil)
		return
	}
	if err != nil {
		slog.Error("move local media failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not move files", nil)
		return
	}
	utils.WriteSuccess(w, map[string]int{"moved": moved})
}

// OpenInFinder opens the Local media library folder on disk in Finder / the file manager.
func (h *LocalMediaHandler) OpenInFinder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	folderID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || folderID <= 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}
	if _, err := h.repo.GetFolder(r.Context(), userID, folderID); err != nil {
		if errors.Is(err, repository.ErrLocalMediaFolderNotFound) {
			utils.WriteError(w, http.StatusNotFound, "folder not found", nil)
			return
		}
		slog.Error("get local media folder for open failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not open folder", nil)
		return
	}
	dir := h.userDir(h.downloadDir(r, userID), userID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("create local media dir failed", "path", dir, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not create library folder", nil)
		return
	}
	if err := openInFileManager(dir); err != nil {
		slog.Error("open local media in finder failed", "path", dir, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not open Finder", nil)
		return
	}
	utils.WriteSuccess(w, map[string]string{"path": dir})
}

// ListItems lists media in a Local media folder.
func (h *LocalMediaHandler) ListItems(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	folderID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}
	items, err := h.repo.ListItems(r.Context(), userID, folderID)
	if errors.Is(err, repository.ErrLocalMediaFolderNotFound) {
		utils.WriteError(w, http.StatusNotFound, err.Error(), nil)
		return
	}
	if err != nil {
		slog.Error("list local media items failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load items", nil)
		return
	}
	out := make([]localMediaItemJSON, 0, len(items))
	for _, m := range items {
		out = append(out, h.itemView(userID, m))
	}
	utils.WriteSuccess(w, out)
}

type copyRequest struct {
	RelPaths []string `json:"rel_paths"`
}

type copyErrorJSON struct {
	RelPath string `json:"rel_path"`
	Error   string `json:"error"`
}

type copyResultJSON struct {
	Copied  []localMediaItemJSON `json:"copied"`
	Skipped []string             `json:"skipped"`
	Errors  []copyErrorJSON      `json:"errors"`
}

// CopyIntoFolder starts a background copy of camera files into a Local media folder.
func (h *LocalMediaHandler) CopyIntoFolder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	folderID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}
	if _, err := h.repo.GetFolder(r.Context(), userID, folderID); err != nil {
		if errors.Is(err, repository.ErrLocalMediaFolderNotFound) {
			utils.WriteError(w, http.StatusNotFound, "create a Local media folder first", nil)
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "could not load folder", nil)
		return
	}

	var body copyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.RelPaths) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "rel_paths required", nil)
		return
	}
	if len(body.RelPaths) > 500 {
		utils.WriteError(w, http.StatusBadRequest, "too many files in one copy (max 500)", nil)
		return
	}

	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil || !cr.HasRoot {
		utils.WriteError(w, http.StatusFailedDependency, "no camera folder chosen", nil)
		return
	}
	if !service.CameraRootAvailable(cr.Root) {
		utils.WriteError(w, http.StatusFailedDependency, "camera folder is not available — reconnect the card", nil)
		return
	}

	paths := make([]string, 0, len(body.RelPaths))
	for _, rel := range body.RelPaths {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel != "" {
			paths = append(paths, rel)
		}
	}
	job := h.jobs.Create(userID, folderID, len(paths))
	dir := h.downloadDir(r, userID)
	root := cr.Root
	go h.runCopyJob(job, userID, folderID, root, dir, paths)
	utils.WriteSuccess(w, job.Snapshot())
}

// GetCopyJob returns progress for a background copy.
func (h *LocalMediaHandler) GetCopyJob(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	id := chi.URLParam(r, "id")
	job := h.jobs.Get(userID, id)
	if job == nil {
		utils.WriteError(w, http.StatusNotFound, "copy job not found", nil)
		return
	}
	utils.WriteSuccess(w, job.Snapshot())
}

func (h *LocalMediaHandler) runCopyJob(job *service.CopyJob, userID, folderID int64, cameraRoot, downloadDir string, paths []string) {
	mu := h.copyLock(userID)
	mu.Lock()
	defer mu.Unlock()

	job.SetRunning()
	ctx := context.Background()
	userDir := h.userDir(downloadDir, userID)
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		job.Finish("error", "could not create library folder")
		return
	}

	for _, rel := range paths {
		src, err := service.ResolveUnderRoot(cameraRoot, rel)
		if err != nil {
			job.MarkFailed(rel, "invalid path")
			continue
		}
		fi, err := os.Stat(src)
		if err != nil || !fi.Mode().IsRegular() {
			job.MarkFailed(rel, "file not found")
			continue
		}
		base := filepath.Base(src)
		ext := strings.ToLower(filepath.Ext(base))
		kind := service.MediaType(base)
		if kind != "image" && kind != "video" {
			job.MarkFailed(rel, "unsupported type")
			continue
		}

		item, err := h.repo.InsertItemPlaceholder(ctx, repository.InsertItemParams{
			UserID: userID, FolderID: folderID, OriginalName: base, Kind: kind,
			ByteSize: fi.Size(), SourceRelPath: rel, SourceRoot: cameraRoot,
		}, ext)
		if errors.Is(err, repository.ErrLocalMediaNameTaken) {
			job.MarkSkipped(rel)
			continue
		}
		if err != nil {
			job.MarkFailed(rel, "could not save")
			continue
		}

		dest := h.itemPath(downloadDir, userID, item.StoredName)
		tmp := dest + ".tmp"
		if err := copyFileAtomic(src, tmp, dest); err != nil {
			_, _ = h.repo.DeleteItem(ctx, userID, item.ID)
			job.MarkFailed(rel, err.Error())
			continue
		}
		job.MarkCopied(base, fi.Size())
	}
	job.Finish("done", "")
}

func copyFileAtomic(src, tmp, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if syncErr != nil {
		os.Remove(tmp)
		return syncErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// DeleteItem removes one Local media file.
func (h *LocalMediaHandler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid id", nil)
		return
	}
	item, err := h.repo.DeleteItem(r.Context(), userID, id)
	if errors.Is(err, repository.ErrLocalMediaNotFound) {
		utils.WriteError(w, http.StatusNotFound, err.Error(), nil)
		return
	}
	if err != nil {
		slog.Error("delete local media failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not delete", nil)
		return
	}
	dir := h.downloadDir(r, userID)
	path := h.itemPath(dir, userID, item.StoredName)
	if path != "" {
		_ = os.Remove(path)
	}
	utils.WriteSuccess(w, map[string]string{"ok": "true"})
}

// Media serves a signed Local media file.
func (h *LocalMediaHandler) Media(w http.ResponseWriter, r *http.Request) {
	userID, err1 := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	itemID, err2 := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	file := chi.URLParam(r, "file")
	q := r.URL.Query()
	if err1 != nil || err2 != nil || file == "" || !h.media.VerifyLocalMedia(userID, itemID, file, q.Get("exp"), q.Get("sig")) {
		http.Error(w, "link expired or invalid", http.StatusForbidden)
		return
	}
	dir := h.downloadDir(r, userID)
	path := h.itemPath(dir, userID, file)
	if path == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(path), fi.ModTime(), f)
}

// Thumb serves a JPEG thumbnail (generated + cached) for a Local media file.
func (h *LocalMediaHandler) Thumb(w http.ResponseWriter, r *http.Request) {
	userID, err1 := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	itemID, err2 := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	file := chi.URLParam(r, "file")
	q := r.URL.Query()
	if err1 != nil || err2 != nil || file == "" || !h.media.VerifyLocalMediaThumb(userID, itemID, file, q.Get("exp"), q.Get("sig")) {
		http.Error(w, "link expired or invalid", http.StatusForbidden)
		return
	}
	dir := h.downloadDir(r, userID)
	src := h.itemPath(dir, userID, file)
	if src == "" {
		http.NotFound(w, r)
		return
	}
	fi, err := os.Stat(src)
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	cacheKey := fmt.Sprintf("lm/%d/%s", itemID, file)
	thumb, err := service.EnsureCameraThumbSingleFlight(
		r.Context(), dir, userID, src, cacheKey, fi.Size(), fi.ModTime().UnixNano(),
	)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Warn("local media thumb failed", "item", itemID, "file", file, "err", err)
		if service.MediaType(file) == "image" {
			f, openErr := os.Open(src)
			if openErr != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			w.Header().Set("Cache-Control", "private, max-age=3600")
			http.ServeContent(w, r, filepath.Base(src), fi.ModTime(), f)
			return
		}
		http.Error(w, "thumbnail unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	f, err := os.Open(thumb)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	tfi, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filepath.Base(thumb), tfi.ModTime(), f)
}
