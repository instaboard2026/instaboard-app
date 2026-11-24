package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"instaboard/internal/middleware"
	"instaboard/internal/models"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

// AddToDumpRequest is the body for POST /api/dump.
type AddToDumpRequest struct {
	URL string `json:"url"`
}

// saveResponse is a save plus what's in the download folder for it (null = not downloaded).
type saveResponse struct {
	models.Save
	Download *service.DownloadState `json:"download"`
}

// downloadsInfo is returned by the /api/downloads endpoints.
type downloadsInfo struct {
	Dir             string `json:"dir"`
	IsDefault       bool   `json:"is_default"`
	Exists          bool   `json:"exists"`
	ToolsAvailable  bool   `json:"tools_available"`
	PickerAvailable bool   `json:"picker_available"`
	CookiesBrowser  string `json:"cookies_browser"`
	FreeBytes       uint64 `json:"free_bytes,omitempty"`
	TotalBytes      uint64 `json:"total_bytes,omitempty"`
}

// DumpHandler handles the user's Dump and downloading its links (authenticated).
type DumpHandler struct {
	repo           *repository.SaveRepo
	settings       *repository.SettingsRepo
	downloader     *service.Downloader
	media          *service.MediaSigner
	local          *repository.LocalMediaRepo
	splitRepo      *repository.SplitRepo
	splitter       *service.SplitService
	defaultDir     string
	cookiesBrowser string
}

// NewDumpHandler creates a new DumpHandler. defaultDir is used until a user picks their own folder.
func NewDumpHandler(
	repo *repository.SaveRepo,
	settings *repository.SettingsRepo,
	downloader *service.Downloader,
	media *service.MediaSigner,
	local *repository.LocalMediaRepo,
	splitRepo *repository.SplitRepo,
	splitter *service.SplitService,
	defaultDir, cookiesBrowser string,
) *DumpHandler {
	return &DumpHandler{
		repo: repo, settings: settings, downloader: downloader, media: media, local: local,
		splitRepo: splitRepo, splitter: splitter,
		defaultDir: defaultDir, cookiesBrowser: cookiesBrowser,
	}
}

// withMedia adds signed URLs and the on-disk path for the save's downloaded files.
func (h *DumpHandler) withMedia(userID int64, dir string, saveID uint64, state *service.DownloadState) *service.DownloadState {
	if state == nil {
		return nil
	}
	state.Media = h.media.Items(userID, saveID, h.downloader.MediaFiles(dir, saveID))
	state.Path = h.downloader.RevealPath(dir, saveID)
	return state
}

// downloadDir returns the user's download folder (read per request, so changes apply without a restart).
func (h *DumpHandler) downloadDir(r *http.Request, userID int64) (dir string, isDefault bool) {
	custom, err := h.settings.DownloadDir(r.Context(), userID)
	if err != nil {
		slog.Error("load download dir failed", "user_id", userID, "err", err)
	}
	if custom == "" {
		return h.defaultDir, true
	}
	return custom, false
}

func (h *DumpHandler) info(r *http.Request, userID int64) downloadsInfo {
	dir, isDefault := h.downloadDir(r, userID)
	fi, err := os.Stat(dir)
	out := downloadsInfo{
		Dir:             dir,
		IsDefault:       isDefault,
		Exists:          (err == nil && fi.IsDir()) || isDefault,
		ToolsAvailable:  h.downloader.Available(),
		PickerAvailable: service.PickerAvailable(),
		CookiesBrowser:  h.cookiesBrowser,
	}
	spacePath := dir
	if err != nil {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			spacePath = home
		} else {
			spacePath = "/"
		}
	}
	if ds, dsErr := service.DiskSpaceForPath(spacePath); dsErr == nil {
		out.FreeBytes = ds.FreeBytes
		out.TotalBytes = ds.TotalBytes
	}
	return out
}

// saveViews turns saves into API responses with their download state and signed media URLs.
func (h *DumpHandler) saveViews(r *http.Request, userID int64, saves []models.Save) []saveResponse {
	dir, _ := h.downloadDir(r, userID)
	out := make([]saveResponse, 0, len(saves))
	for _, s := range saves {
		out = append(out, h.saveView(userID, dir, s))
	}
	return out
}

func (h *DumpHandler) saveView(userID int64, dir string, s models.Save) saveResponse {
	return saveResponse{Save: s, Download: h.withMedia(userID, dir, s.ID, h.downloader.Status(dir, s.ID))}
}

// removeDownloadFiles deletes a save's files from the user's download folder.
func (h *DumpHandler) removeDownloadFiles(r *http.Request, userID int64, saveID uint64) {
	dir, _ := h.downloadDir(r, userID)
	_ = h.downloader.RemoveFiles(dir, saveID)
}

// downloadStats is returned by GET /api/stats (all of the user's saves, Home and folders).
type downloadStats struct {
	TotalDownloads int            `json:"total_downloads"`
	StorageBytes   int64          `json:"storage_bytes"`
	BySource       map[string]int `json:"by_source"`
}

// Stats returns how many saves are downloaded (overall and per source) and the disk space they use.
func (h *DumpHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	saves, err := h.repo.ListAll(r.Context(), userID)
	if err != nil {
		slog.Error("list saves for stats failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to load stats", nil)
		return
	}
	dir, _ := h.downloadDir(r, userID)
	stats := downloadStats{BySource: map[string]int{
		service.SourceInstagram: 0,
		service.SourceYouTube:   0,
		service.SourcePinterest: 0,
		service.SourceLocal:     0,
	}}
	for _, s := range saves {
		if h.downloader.Status(dir, s.ID) == nil {
			continue
		}
		stats.TotalDownloads++
		stats.BySource[s.Source]++
		stats.StorageBytes += h.downloader.DiskUsage(dir, s.ID)
	}
	utils.WriteSuccess(w, stats)
}

// GetDump returns the logged-in user's Dump saves (not in any folder), newest first.
func (h *DumpHandler) GetDump(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	saves, err := h.repo.ListDump(r.Context(), userID)
	if err != nil {
		slog.Error("list dump failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to load dump", nil)
		return
	}
	utils.WriteSuccess(w, h.saveViews(r, userID, saves))
}

// AddToDump validates and saves a pasted Instagram, Pinterest or YouTube link to the Dump.
func (h *DumpHandler) AddToDump(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	h.addLink(w, r, userID, nil)
}

type dumpOrderRequest struct {
	IDs []uint64 `json:"ids"`
}

// Reorder sets the Home board order of the user's Dump saves.
func (h *DumpHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	var req dumpOrderRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}
	if err := h.repo.ReorderDump(r.Context(), userID, req.IDs); err != nil {
		if errors.Is(err, repository.ErrSaveOrder) {
			utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		slog.Error("reorder dump failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "reorder dump failed", nil)
		return
	}
	saves, err := h.repo.ListDump(r.Context(), userID)
	if err != nil {
		slog.Error("list dump failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to load dump", nil)
		return
	}
	utils.WriteSuccess(w, h.saveViews(r, userID, saves))
}

// addLink reads {url} from the body and saves it into folderID (nil = Dump).
func (h *DumpHandler) addLink(w http.ResponseWriter, r *http.Request, userID int64, folderID *int64) {
	var req AddToDumpRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	url, source, err := service.NormalizeLink(req.URL)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error(), []utils.FieldError{{Field: "url", Message: err.Error()}})
		return
	}

	save, err := h.repo.Insert(r.Context(), userID, url, source, "web", folderID)
	switch {
	case errors.Is(err, repository.ErrDuplicateSave):
		utils.WriteError(w, http.StatusConflict, "this link is already saved", nil)
		return
	case errors.Is(err, repository.ErrFolderNotFound):
		utils.WriteError(w, http.StatusNotFound, "folder not found", nil)
		return
	case err != nil:
		slog.Error("add link failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to save link", nil)
		return
	}
	dir, _ := h.downloadDir(r, userID)
	utils.WriteJSON(w, http.StatusCreated, utils.APIResponse{Status: "SUCCESS", Data: h.saveView(userID, dir, *save)})
}

// Download fetches a saved Instagram reel or post into the download folder.
func (h *DumpHandler) Download(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid save id", nil)
		return
	}

	save, err := h.repo.Get(r.Context(), userID, id)
	if errors.Is(err, repository.ErrSaveNotFound) {
		utils.WriteError(w, http.StatusNotFound, "save not found", nil)
		return
	}
	if err != nil {
		slog.Error("get save failed", "user_id", userID, "save_id", id, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to load save", nil)
		return
	}
	if save.Source != service.SourceInstagram {
		utils.WriteError(w, http.StatusBadRequest, "downloads currently support Instagram links only", nil)
		return
	}

	dir, isDefault := h.downloadDir(r, userID)
	if !isDefault {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			utils.WriteError(w, http.StatusConflict, "the download folder "+dir+" was renamed, moved or deleted. Choose a new folder", nil)
			return
		}
	}
	state, err := h.downloader.Download(r.Context(), dir, save.ID, save.URL)
	if err != nil {
		slog.Warn("download failed", "user_id", userID, "save_id", id, "err", err)
		status := http.StatusBadGateway
		if errors.Is(err, service.ErrToolMissing) {
			status = http.StatusServiceUnavailable
		}
		utils.WriteError(w, status, err.Error(), nil)
		return
	}
	if eng := state.Engagement; eng != nil {
		if updated, err := h.repo.UpdateEngagement(r.Context(), userID, save.ID,
			eng.LikeCount, eng.CommentCount, eng.RepostCount, eng.ShareCount, eng.PlayCount,
		); err != nil {
			slog.Warn("save engagement failed", "user_id", userID, "save_id", id, "err", err)
		} else {
			save = updated
		}
	}
	utils.WriteSuccess(w, saveResponse{Save: *save, Download: h.withMedia(userID, dir, save.ID, state)})
}

// PromoteFromLocalMedia moves Local media files into Home or a Dump folder as local save cards.
func (h *DumpHandler) PromoteFromLocalMedia(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	if h.local == nil {
		utils.WriteError(w, http.StatusInternalServerError, "local media unavailable", nil)
		return
	}
	var body struct {
		IDs      []uint64 `json:"ids"`
		FolderID *int64   `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "ids required", nil)
		return
	}
	if body.FolderID != nil && *body.FolderID <= 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}

	dir, _ := h.downloadDir(r, userID)
	lmDir := filepath.Join(dir, "local-media", strconv.FormatInt(userID, 10))
	out := make([]saveResponse, 0, len(body.IDs))

	for _, id := range body.IDs {
		item, err := h.local.GetItem(r.Context(), userID, id)
		if errors.Is(err, repository.ErrLocalMediaNotFound) {
			utils.WriteError(w, http.StatusNotFound, "local media file not found", nil)
			return
		}
		if err != nil {
			slog.Error("get local media for promote failed", "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "could not move files", nil)
			return
		}
		base := filepath.Base(item.StoredName)
		if base != item.StoredName || strings.Contains(base, "..") {
			utils.WriteError(w, http.StatusBadRequest, "invalid local media file", nil)
			return
		}
		src := filepath.Join(lmDir, base)

		url, err := newLocalURL()
		if err != nil {
			slog.Error("local url failed", "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "failed to create card", nil)
			return
		}
		save, err := h.repo.Insert(r.Context(), userID, url, service.SourceLocal, "local-media", body.FolderID)
		switch {
		case errors.Is(err, repository.ErrFolderNotFound):
			utils.WriteError(w, http.StatusNotFound, "folder not found", nil)
			return
		case err != nil:
			slog.Error("insert promoted save failed", "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "failed to create card", nil)
			return
		}

		state, storeErr := h.downloader.AdoptLocalFile(dir, save.ID, src)
		if storeErr != nil {
			h.removeDownloadFiles(r, userID, save.ID)
			_ = h.repo.Delete(r.Context(), userID, save.ID)
			slog.Error("adopt local media failed", "item", id, "err", storeErr)
			utils.WriteError(w, http.StatusInternalServerError, "could not move file into folder", nil)
			return
		}
		if _, err := h.local.DeleteItem(r.Context(), userID, id); err != nil {
			slog.Warn("promoted save but local media row remains", "item", id, "err", err)
		}
		out = append(out, saveResponse{Save: *save, Download: h.withMedia(userID, dir, save.ID, state)})
	}

	utils.WriteJSON(w, http.StatusCreated, utils.APIResponse{Status: "SUCCESS", Data: out})
}

// CreateFromSplit copies a Split master or clip into Home or a Dump folder as a local save card.
// The split project and its files are left unchanged.
func (h *DumpHandler) CreateFromSplit(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	if h.splitRepo == nil || h.splitter == nil {
		utils.WriteError(w, http.StatusInternalServerError, "split unavailable", nil)
		return
	}
	var body struct {
		ProjectID uint64  `json:"project_id"`
		ClipID    *uint64 `json:"clip_id"`
		FolderID  *int64  `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ProjectID == 0 {
		utils.WriteError(w, http.StatusBadRequest, "project_id required", nil)
		return
	}
	if body.FolderID != nil && *body.FolderID <= 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid folder id", nil)
		return
	}
	if body.ClipID != nil && *body.ClipID == 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid clip id", nil)
		return
	}

	if _, err := h.splitRepo.GetProject(r.Context(), userID, body.ProjectID); errors.Is(err, repository.ErrSplitProjectNotFound) {
		utils.WriteError(w, http.StatusNotFound, "split project not found", nil)
		return
	} else if err != nil {
		slog.Error("get split project for export failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not export clip", nil)
		return
	}

	dir, _ := h.downloadDir(r, userID)
	var src string
	if body.ClipID == nil {
		src = h.splitter.MasterPath(dir, userID, body.ProjectID)
	} else {
		clips, err := h.splitRepo.ListClips(r.Context(), body.ProjectID)
		if err != nil {
			slog.Error("list split clips for export failed", "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "could not export clip", nil)
			return
		}
		var fileName string
		for _, c := range clips {
			if c.ID == *body.ClipID {
				fileName = c.FileName
				break
			}
		}
		if fileName == "" {
			utils.WriteError(w, http.StatusNotFound, "split clip not found", nil)
			return
		}
		src = h.splitter.ClipPath(dir, userID, body.ProjectID, fileName)
		if src == "" {
			utils.WriteError(w, http.StatusBadRequest, "invalid split clip file", nil)
			return
		}
	}
	if fi, err := os.Stat(src); err != nil || !fi.Mode().IsRegular() {
		utils.WriteError(w, http.StatusNotFound, "split media file not found", nil)
		return
	}

	url, err := newLocalURL()
	if err != nil {
		slog.Error("local url failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to create card", nil)
		return
	}
	save, err := h.repo.Insert(r.Context(), userID, url, service.SourceLocal, "split", body.FolderID)
	switch {
	case errors.Is(err, repository.ErrFolderNotFound):
		utils.WriteError(w, http.StatusNotFound, "folder not found", nil)
		return
	case err != nil:
		slog.Error("insert split export save failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to create card", nil)
		return
	}

	state, storeErr := h.downloader.CopyLocalFile(dir, save.ID, src)
	if storeErr != nil {
		h.removeDownloadFiles(r, userID, save.ID)
		_ = h.repo.Delete(r.Context(), userID, save.ID)
		slog.Error("copy split media failed", "project", body.ProjectID, "err", storeErr)
		utils.WriteError(w, http.StatusInternalServerError, "could not copy file into folder", nil)
		return
	}

	utils.WriteJSON(w, http.StatusCreated, utils.APIResponse{
		Status: "SUCCESS",
		Data:   saveResponse{Save: *save, Download: h.withMedia(userID, dir, save.ID, state)},
	})
}

// RevealInFinder opens the downloaded file for this save in Finder (macOS) or the file manager.
func (h *DumpHandler) RevealInFinder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		utils.WriteError(w, http.StatusBadRequest, "invalid save id", nil)
		return
	}
	if _, err := h.repo.Get(r.Context(), userID, id); errors.Is(err, repository.ErrSaveNotFound) {
		utils.WriteError(w, http.StatusNotFound, "save not found", nil)
		return
	} else if err != nil {
		slog.Error("get save for reveal failed", "user_id", userID, "save_id", id, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to load save", nil)
		return
	}
	dir, _ := h.downloadDir(r, userID)
	path := h.downloader.RevealPath(dir, id)
	if path == "" {
		utils.WriteError(w, http.StatusNotFound, "no downloaded file to open", nil)
		return
	}
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		utils.WriteError(w, http.StatusNotFound, "file is missing on disk", nil)
		return
	}
	if err := revealInFileManager(path); err != nil {
		slog.Error("reveal in finder failed", "path", path, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not open File Explorer", nil)
		return
	}
	utils.WriteSuccess(w, map[string]string{"path": path})
}

func revealInFileManager(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}

// openInFileManager opens a directory in Finder / Explorer / the file manager.
func openInFileManager(dir string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", dir).Start()
	case "windows":
		return exec.Command("explorer", dir).Start()
	default:
		return exec.Command("xdg-open", dir).Start()
	}
}

// DownloadsInfo returns the user's download folder and whether the download tools are installed.
func (h *DumpHandler) DownloadsInfo(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	utils.WriteSuccess(w, h.info(r, userID))
}

// InstallDownloadTools installs (or reinstalls) gallery-dl, yt-dlp and ffmpeg for this Mac.
func (h *DumpHandler) InstallDownloadTools(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	if err := service.InstallDownloadTools(r.Context()); err != nil {
		slog.Error("install download tools failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	h.downloader.RefreshTools()
	if !h.downloader.Available() {
		utils.WriteError(w, http.StatusInternalServerError, "tools installed but still not found — try quitting Instaboard and opening it again", nil)
		return
	}
	utils.WriteSuccess(w, h.info(r, userID))
}

// UninstallDownloadTools removes managed tools and turns downloads off until the user installs again.
func (h *DumpHandler) UninstallDownloadTools(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	if err := service.UninstallDownloadTools(); err != nil {
		slog.Error("uninstall download tools failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	h.downloader.RefreshTools()
	utils.WriteSuccess(w, h.info(r, userID))
}

// ChooseDownloadFolder opens the native folder picker on this computer and saves the chosen folder.
// The request waits until the user picks or cancels.
func (h *DumpHandler) ChooseDownloadFolder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	current, _ := h.downloadDir(r, userID)
	dir, err := service.ChooseFolder(r.Context(), "Choose where Instaboard saves downloads", current)
	switch {
	case errors.Is(err, service.ErrPickerCancelled):
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	case errors.Is(err, service.ErrPickerBusy):
		utils.WriteError(w, http.StatusConflict, err.Error(), nil)
		return
	case errors.Is(err, service.ErrPickerUnsupported), errors.Is(err, service.ErrFolderNotWritable):
		utils.WriteError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	case err != nil:
		slog.Error("folder picker failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "couldn't open the folder picker", nil)
		return
	}
	if err := h.settings.SetDownloadDir(r.Context(), userID, dir); err != nil {
		slog.Error("save download dir failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to save the folder", nil)
		return
	}
	utils.WriteSuccess(w, h.info(r, userID))
}

// ResetDownloadFolder switches the user back to the default download folder.
func (h *DumpHandler) ResetDownloadFolder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	if err := h.settings.SetDownloadDir(r.Context(), userID, ""); err != nil {
		slog.Error("reset download dir failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to reset the folder", nil)
		return
	}
	utils.WriteSuccess(w, h.info(r, userID))
}

// Media serves a downloaded file using a signed URL from the Dump response (public route; the
// signature binds user, save and file, and expires). Supports range requests for video seeking.
func (h *DumpHandler) Media(w http.ResponseWriter, r *http.Request) {
	userID, err1 := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	saveID, err2 := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	file := chi.URLParam(r, "file")
	q := r.URL.Query()
	if err1 != nil || err2 != nil || !h.media.Verify(userID, saveID, file, q.Get("exp"), q.Get("sig")) {
		http.Error(w, "link expired or invalid", http.StatusForbidden)
		return
	}
	dir, _ := h.downloadDir(r, userID)
	path := h.downloader.MediaPath(dir, saveID, file)
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
	http.ServeContent(w, r, file, fi.ModTime(), f)
}
