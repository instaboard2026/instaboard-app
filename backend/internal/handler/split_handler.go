package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"instaboard/internal/middleware"
	"instaboard/internal/models"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

const (
	maxSplitUploadBytes = 500 << 20 // 500 MiB
	maxSplitMultipart   = 520 << 20
)

// SplitHandler serves the Split video analysis workspace.
type SplitHandler struct {
	repo       *repository.SplitRepo
	saves      *repository.SaveRepo
	settings   *repository.SettingsRepo
	splitter   *service.SplitService
	downloader *service.Downloader
	media      *service.MediaSigner
	defaultDir string
}

// NewSplitHandler creates a SplitHandler.
func NewSplitHandler(
	repo *repository.SplitRepo,
	saves *repository.SaveRepo,
	settings *repository.SettingsRepo,
	splitter *service.SplitService,
	downloader *service.Downloader,
	media *service.MediaSigner,
	defaultDir string,
) *SplitHandler {
	return &SplitHandler{
		repo: repo, saves: saves, settings: settings,
		splitter: splitter, downloader: downloader, media: media, defaultDir: defaultDir,
	}
}

type splitClipJSON struct {
	models.SplitClip
	URL  string `json:"url"`
	Type string `json:"type"`
}

type splitProjectJSON struct {
	models.SplitProject
	MasterURL string          `json:"master_url"`
	ClipCount int             `json:"clip_count"`
	Clips     []splitClipJSON `json:"clips,omitempty"`
}

type createFromSaveRequest struct {
	SourceType string  `json:"source_type"`
	SaveID     uint64  `json:"save_id"`
	File       *string `json:"file"`
}

type cutRequest struct {
	Threshold *float64 `json:"threshold"`
}

func (h *SplitHandler) downloadDir(r *http.Request, userID int64) string {
	custom, err := h.settings.DownloadDir(r.Context(), userID)
	if err != nil {
		slog.Error("load download dir failed", "user_id", userID, "err", err)
	}
	if custom == "" {
		return h.defaultDir
	}
	return custom
}

func (h *SplitHandler) projectView(userID int64, p models.SplitProject, clips []models.SplitClip, includeClips bool) splitProjectJSON {
	view := splitProjectJSON{
		SplitProject: p,
		MasterURL:    h.media.SplitURL(userID, p.ID, p.MasterFile).URL,
		ClipCount:    len(clips),
	}
	if !includeClips {
		return view
	}
	out := make([]splitClipJSON, 0, len(clips))
	for _, c := range clips {
		item := h.media.SplitURL(userID, p.ID, "clips/"+c.FileName)
		out = append(out, splitClipJSON{SplitClip: c, URL: item.URL, Type: item.Type})
	}
	view.Clips = out
	return view
}

// List returns every split project for the user.
func (h *SplitHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	projects, err := h.repo.ListProjects(r.Context(), userID)
	if err != nil {
		slog.Error("list split projects failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load split projects", nil)
		return
	}
	out := make([]splitProjectJSON, 0, len(projects))
	for _, p := range projects {
		n, _ := h.repo.ClipCount(r.Context(), p.ID)
		view := h.projectView(userID, p, nil, false)
		view.ClipCount = n
		out = append(out, view)
	}
	utils.WriteSuccess(w, out)
}

// Get returns one project with clips.
func (h *SplitHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	p, err := h.repo.GetProject(r.Context(), userID, uint64(id))
	if errors.Is(err, repository.ErrSplitProjectNotFound) {
		utils.WriteError(w, http.StatusNotFound, "project not found", nil)
		return
	}
	if err != nil {
		slog.Error("get split project failed", "user_id", userID, "id", id, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load project", nil)
		return
	}
	clips, err := h.repo.ListClips(r.Context(), p.ID)
	if err != nil {
		slog.Error("list split clips failed", "project_id", p.ID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load clips", nil)
		return
	}
	utils.WriteSuccess(w, h.projectView(userID, *p, clips, true))
}

// Create accepts JSON from a dump save, or multipart video upload.
func (h *SplitHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		h.createFromUpload(w, r, userID)
		return
	}
	h.createFromSave(w, r, userID)
}

func (h *SplitHandler) createFromSave(w http.ResponseWriter, r *http.Request, userID int64) {
	var req createFromSaveRequest
	if !decode(w, r, &req) {
		return
	}
	if req.SourceType != "save" || req.SaveID == 0 {
		utils.WriteError(w, http.StatusBadRequest, "source_type must be save with save_id", nil)
		return
	}
	save, err := h.saves.Get(r.Context(), userID, req.SaveID)
	if errors.Is(err, repository.ErrSaveNotFound) {
		utils.WriteError(w, http.StatusNotFound, "save not found", nil)
		return
	}
	if err != nil {
		slog.Error("get save for split failed", "user_id", userID, "save_id", req.SaveID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load save", nil)
		return
	}

	dir := h.downloadDir(r, userID)
	files := h.downloader.MediaFiles(dir, save.ID)
	file, err := pickVideoFile(files, req.File)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	title := save.URL
	p, err := h.repo.CreateProject(r.Context(), userID, "save", &save.ID, &file, &title)
	if err != nil {
		slog.Error("create split project failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not create project", nil)
		return
	}

	durationMs, err := h.splitter.CopyMasterFromSave(dir, userID, p.ID, save.ID, file)
	if err != nil {
		_ = h.repo.DeleteProject(r.Context(), userID, p.ID)
		_ = h.splitter.RemoveProjectDir(dir, userID, p.ID)
		slog.Error("copy master for split failed", "project_id", p.ID, "err", err)
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	_ = h.repo.UpdateAfterIngest(r.Context(), userID, p.ID, durationMs, &title)
	p, _ = h.repo.GetProject(r.Context(), userID, p.ID)
	utils.WriteSuccess(w, h.projectView(userID, *p, nil, false))
}

func (h *SplitHandler) createFromUpload(w http.ResponseWriter, r *http.Request, userID int64) {
	if err := r.ParseMultipartForm(maxSplitMultipart); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "could not read uploaded file", nil)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "drop a video file", nil)
		return
	}
	defer file.Close()
	if hdr.Size > maxSplitUploadBytes {
		utils.WriteError(w, http.StatusBadRequest, "video is too large (max 500 MB)", nil)
		return
	}
	if err := assertVideoUpload(hdr.Filename, hdr.Header.Get("Content-Type")); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	title := filepath.Base(hdr.Filename)
	p, err := h.repo.CreateProject(r.Context(), userID, "upload", nil, nil, &title)
	if err != nil {
		slog.Error("create split upload project failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not create project", nil)
		return
	}

	dir := h.downloadDir(r, userID)
	limited := io.LimitReader(file, maxSplitUploadBytes+1)
	durationMs, err := h.splitter.StoreMasterUpload(dir, userID, p.ID, limited)
	if err != nil {
		_ = h.repo.DeleteProject(r.Context(), userID, p.ID)
		_ = h.splitter.RemoveProjectDir(dir, userID, p.ID)
		slog.Error("store split upload failed", "project_id", p.ID, "err", err)
		utils.WriteError(w, http.StatusBadRequest, "could not store video", nil)
		return
	}
	_ = h.repo.UpdateAfterIngest(r.Context(), userID, p.ID, durationMs, &title)
	p, _ = h.repo.GetProject(r.Context(), userID, p.ID)
	utils.WriteSuccess(w, h.projectView(userID, *p, nil, false))
}

// Cut runs scene detection and writes clip files.
func (h *SplitHandler) Cut(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	var req cutRequest
	if r.ContentLength > 0 {
		if !decode(w, r, &req) {
			return
		}
	}
	threshold := 5.0
	if req.Threshold != nil && *req.Threshold > 0 {
		threshold = *req.Threshold
	}

	p, err := h.repo.GetProject(r.Context(), userID, uint64(id))
	if errors.Is(err, repository.ErrSplitProjectNotFound) {
		utils.WriteError(w, http.StatusNotFound, "project not found", nil)
		return
	}
	if err != nil {
		slog.Error("get split project for cut failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load project", nil)
		return
	}
	if !service.FFmpegAvailable() {
		utils.WriteError(w, http.StatusBadRequest, "ffmpeg is not installed — install download tools first", nil)
		return
	}

	dir := h.downloadDir(r, userID)
	_ = h.repo.UpdateStatus(r.Context(), userID, p.ID, "detecting", nil)

	var durationMs int64
	if p.DurationMs != nil {
		durationMs = *p.DurationMs
	}

	_ = h.repo.UpdateStatus(r.Context(), userID, p.ID, "splitting", nil)
	clips, err := h.splitter.DetectAndSplit(r.Context(), dir, userID, p.ID, durationMs, threshold)
	if err != nil {
		msg := err.Error()
		_ = h.repo.UpdateStatus(r.Context(), userID, p.ID, "error", &msg)
		slog.Error("split cut failed", "project_id", p.ID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, msg, nil)
		return
	}

	if err := h.repo.ReplaceClips(r.Context(), userID, p.ID, threshold, clips); err != nil {
		msg := "could not save clips"
		_ = h.repo.UpdateStatus(r.Context(), userID, p.ID, "error", &msg)
		slog.Error("replace split clips failed", "project_id", p.ID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, msg, nil)
		return
	}

	p, _ = h.repo.GetProject(r.Context(), userID, p.ID)
	stored, _ := h.repo.ListClips(r.Context(), p.ID)
	utils.WriteSuccess(w, h.projectView(userID, *p, stored, true))
}

// Delete removes the project and its files.
func (h *SplitHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	dir := h.downloadDir(r, userID)
	if err := h.repo.DeleteProject(r.Context(), userID, uint64(id)); errors.Is(err, repository.ErrSplitProjectNotFound) {
		utils.WriteError(w, http.StatusNotFound, "project not found", nil)
		return
	} else if err != nil {
		slog.Error("delete split project failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not delete project", nil)
		return
	}
	_ = h.splitter.RemoveProjectDir(dir, userID, uint64(id))
	utils.WriteSuccess(w, map[string]string{"ok": "true"})
}

// Media serves a signed split project file (master or clip).
func (h *SplitHandler) Media(w http.ResponseWriter, r *http.Request) {
	userID, err1 := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	projectID, err2 := strconv.ParseUint(chi.URLParam(r, "projectId"), 10, 64)
	file := filepath.ToSlash(strings.TrimPrefix(chi.URLParam(r, "*"), "/"))
	q := r.URL.Query()
	if err1 != nil || err2 != nil || file == "" || !h.media.VerifySplit(userID, projectID, file, q.Get("exp"), q.Get("sig")) {
		http.Error(w, "link expired or invalid", http.StatusForbidden)
		return
	}
	dir := h.downloadDir(r, userID)
	path := h.splitter.MediaPath(dir, userID, projectID, file)
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

func pickVideoFile(files []string, want *string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("download the video on Home first")
	}
	if want != nil && *want != "" {
		for _, f := range files {
			if f == *want {
				if service.MediaType(f) != "video" {
					return "", fmt.Errorf("selected file is not a video")
				}
				return f, nil
			}
		}
		return "", fmt.Errorf("selected media file not found")
	}
	for _, f := range files {
		if service.MediaType(f) == "video" {
			return f, nil
		}
	}
	return "", fmt.Errorf("this card has no video to split")
}

func assertVideoUpload(filename, contentType string) error {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4", ".mov", ".m4v", ".webm":
		return nil
	}
	if contentType != "" {
		mt, _, _ := mime.ParseMediaType(contentType)
		if strings.HasPrefix(mt, "video/") {
			return nil
		}
	}
	return fmt.Errorf("drop an mp4, mov, m4v, or webm video")
}
