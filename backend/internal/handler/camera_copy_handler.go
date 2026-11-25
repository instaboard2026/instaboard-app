package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"instaboard/internal/middleware"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

// CameraCopyHandler copies camera / SD card files into Home or a folder as cards.
type CameraCopyHandler struct {
	dump    *DumpHandler
	folders *repository.FolderRepo
	copies  *repository.CameraCopyRepo
	jobs    *service.CopyJobStore
	copyMu  sync.Map // userID -> *sync.Mutex
}

// NewCameraCopyHandler creates a CameraCopyHandler.
func NewCameraCopyHandler(dump *DumpHandler, folders *repository.FolderRepo, copies *repository.CameraCopyRepo, jobs *service.CopyJobStore) *CameraCopyHandler {
	return &CameraCopyHandler{dump: dump, folders: folders, copies: copies, jobs: jobs}
}

type cameraCopyRequest struct {
	RelPaths []string `json:"rel_paths"`
	// FolderID nil copies to Home.
	FolderID *int64 `json:"folder_id"`
}

// Start begins a background copy of camera files into a folder (or Home); poll with Job.
func (h *CameraCopyHandler) Start(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	var body cameraCopyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || len(body.RelPaths) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "rel_paths required", nil)
		return
	}
	if len(body.RelPaths) > 500 {
		utils.WriteError(w, http.StatusBadRequest, "too many files in one copy (max 500)", nil)
		return
	}
	var jobFolder int64
	if body.FolderID != nil {
		if _, err := h.folders.Get(r.Context(), userID, *body.FolderID); err != nil {
			if errors.Is(err, repository.ErrFolderNotFound) {
				utils.WriteError(w, http.StatusNotFound, "folder not found", nil)
				return
			}
			utils.WriteError(w, http.StatusInternalServerError, "could not load folder", nil)
			return
		}
		jobFolder = *body.FolderID
	}
	cr, err := h.dump.settings.GetCameraRoot(r.Context(), userID)
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
		if rel = filepath.ToSlash(strings.TrimSpace(rel)); rel != "" {
			paths = append(paths, rel)
		}
	}
	job := h.jobs.Create(userID, jobFolder, len(paths))
	dir, _ := h.dump.downloadDir(r, userID)
	go h.run(job, userID, body.FolderID, cr.Root, dir, paths)
	utils.WriteSuccess(w, job.Snapshot())
}

// Job returns progress for a background copy.
func (h *CameraCopyHandler) Job(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	job := h.jobs.Get(userID, chi.URLParam(r, "id"))
	if job == nil {
		utils.WriteError(w, http.StatusNotFound, "copy job not found", nil)
		return
	}
	utils.WriteSuccess(w, job.Snapshot())
}

func (h *CameraCopyHandler) run(job *service.CopyJob, userID int64, folderID *int64, cameraRoot, dir string, paths []string) {
	v, _ := h.copyMu.LoadOrStore(userID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	job.SetRunning()
	ctx := context.Background()
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
		if k := service.MediaType(base); k != "image" && k != "video" {
			job.MarkFailed(rel, "unsupported type")
			continue
		}
		if done, err := h.copies.Has(ctx, userID, base); err == nil && done {
			job.MarkSkipped(rel)
			continue
		}
		if _, err := h.dump.saveLocalFile(ctx, userID, dir, folderID, src, base, "camera", false); err != nil {
			slog.Warn("camera copy failed", "file", rel, "err", err)
			job.MarkFailed(rel, "could not copy")
			continue
		}
		job.MarkCopied(base, fi.Size())
	}
	job.Finish("done", "")
}
