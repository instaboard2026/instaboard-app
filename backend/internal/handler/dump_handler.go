package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

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
}

// DumpHandler handles the user's Dump and downloading its links (authenticated).
type DumpHandler struct {
	repo           *repository.SaveRepo
	settings       *repository.SettingsRepo
	downloader     *service.Downloader
	media          *service.MediaSigner
	defaultDir     string
	cookiesBrowser string
}

// NewDumpHandler creates a new DumpHandler. defaultDir is used until a user picks their own folder.
func NewDumpHandler(repo *repository.SaveRepo, settings *repository.SettingsRepo, downloader *service.Downloader, media *service.MediaSigner, defaultDir, cookiesBrowser string) *DumpHandler {
	return &DumpHandler{repo: repo, settings: settings, downloader: downloader, media: media, defaultDir: defaultDir, cookiesBrowser: cookiesBrowser}
}

// withMedia adds signed URLs for the save's downloaded files to its download state.
func (h *DumpHandler) withMedia(userID int64, dir string, saveID uint64, state *service.DownloadState) *service.DownloadState {
	if state == nil {
		return nil
	}
	state.Media = h.media.Items(userID, saveID, h.downloader.MediaFiles(dir, saveID))
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
	return downloadsInfo{
		Dir:             dir,
		IsDefault:       isDefault,
		Exists:          (err == nil && fi.IsDir()) || isDefault,
		ToolsAvailable:  h.downloader.Available(),
		PickerAvailable: service.PickerAvailable(),
		CookiesBrowser:  h.cookiesBrowser,
	}
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
	utils.WriteSuccess(w, saveResponse{Save: *save, Download: h.withMedia(userID, dir, save.ID, state)})
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
