package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"instaboard/internal/middleware"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

// CameraHandler serves Manage camera (browse SD / folder, signed previews).
type CameraHandler struct {
	settings   *repository.SettingsRepo
	copies     *repository.CameraCopyRepo
	index      *repository.CameraIndexRepo
	scanner    *service.CameraScanner
	media      *service.MediaSigner
	tray       *repository.AirDropTrayRepo
	trash      *repository.CameraTrashRepo
	defaultDir string
}

// NewCameraHandler creates a CameraHandler.
func NewCameraHandler(
	settings *repository.SettingsRepo,
	copies *repository.CameraCopyRepo,
	index *repository.CameraIndexRepo,
	scanner *service.CameraScanner,
	media *service.MediaSigner,
	defaultDir string,
) *CameraHandler {
	return &CameraHandler{
		settings: settings, copies: copies, index: index,
		scanner: scanner, media: media, defaultDir: defaultDir,
	}
}

func (h *CameraHandler) downloadDir(r *http.Request, userID int64) string {
	custom, err := h.settings.DownloadDir(r.Context(), userID)
	if err != nil {
		slog.Error("load download dir failed", "user_id", userID, "err", err)
	}
	if custom == "" {
		return h.defaultDir
	}
	return custom
}

type cameraStatusJSON struct {
	Root         string `json:"root,omitempty"`
	Label        string `json:"label,omitempty"`
	OpenedAt     string `json:"opened_at,omitempty"`
	Available    bool   `json:"available"`
	HasRoot      bool   `json:"has_root"`
	Scan         string `json:"scan"`
	IndexedCount int    `json:"indexed_count"`
	Truncated    bool   `json:"truncated"`
	TimedOut     bool   `json:"timed_out"`
	ScanError    string `json:"scan_error,omitempty"`
	AllCount     int    `json:"all_count"`
	VideoCount   int    `json:"video_count"`
	ImageCount   int    `json:"image_count"`
	JunkCount    int    `json:"junk_count"`
	FreeBytes    uint64 `json:"free_bytes,omitempty"`
	// Ejectable is true when the folder is on a connected external card/drive (Safely eject shows).
	Ejectable  bool   `json:"ejectable"`
	TotalBytes uint64 `json:"total_bytes,omitempty"`
	// AirDropCount is how many files are in the card's AIRDROP/ folder; AirDropSending while its window is open.
	AirDropCount   int  `json:"airdrop_count"`
	AirDropSending bool `json:"airdrop_sending"`
}

type cameraItemJSON struct {
	RelPath  string `json:"rel_path"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	ByteSize int64  `json:"byte_size"`
	MtimeNs  int64  `json:"mtime_ns"`
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url"`
	Copied   bool   `json:"copied"`
	// Sent is true once the file was AirDropped.
	Sent bool `json:"sent"`
	// DurationMs is the video length once its thumbnail has been made (0 = not known yet).
	DurationMs int64 `json:"duration_ms,omitempty"`
}

type cameraItemsPageJSON struct {
	Root       string           `json:"root"`
	Label      string           `json:"label"`
	Items      []cameraItemJSON `json:"items"`
	Offset     int              `json:"offset"`
	Limit      int              `json:"limit"`
	Total      int              `json:"total"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
	Scan       string           `json:"scan"`
	Truncated  bool             `json:"truncated"`
	TimedOut   bool             `json:"timed_out"`
}

type cameraOpenJSON struct {
	Root         string `json:"root"`
	Label        string `json:"label"`
	Scan         string `json:"scan"`
	IndexedCount int    `json:"indexed_count"`
}

// Status returns the persisted camera root and scan state.
func (h *CameraHandler) Status(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil {
		slog.Error("get camera root failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load camera folder", nil)
		return
	}
	meta, err := h.index.GetScanMeta(r.Context(), userID)
	if err != nil {
		slog.Error("get scan meta failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load scan status", nil)
		return
	}
	if h.scanner.IsBusy(userID) && meta.Status != "running" {
		meta.Status = "running"
	}
	scope := repository.ScopeMain
	if r.URL.Query().Get("folder") == repository.ScopeAirDrop {
		scope = repository.ScopeAirDrop
	}
	all, video, image, junk, _ := h.index.KindCounts(r.Context(), userID, scope)
	airdropCount, _ := h.index.CountByKind(r.Context(), userID, "all", repository.ScopeAirDrop)
	sending := false
	if v, ok := airdropSending.Load(userID); ok {
		sending = v.(bool)
	}
	available := cr.HasRoot && service.CameraRootAvailable(cr.Root)
	out := cameraStatusJSON{
		Root: cr.Root, Label: cr.Label, OpenedAt: cr.OpenedAt,
		HasRoot: cr.HasRoot, Available: available,
		Scan: meta.Status, IndexedCount: meta.IndexedCount,
		Truncated: meta.Truncated, TimedOut: meta.TimedOut, ScanError: meta.ErrorMessage,
		AllCount: all, VideoCount: video, ImageCount: image, JunkCount: junk,
		AirDropCount: airdropCount, AirDropSending: sending,
	}
	if available {
		out.Ejectable = service.VolumeMountPoint(cr.Root) != ""
		if ds, err := service.DiskSpaceForPath(cr.Root); err == nil {
			out.FreeBytes = ds.FreeBytes
			out.TotalBytes = ds.TotalBytes
		}
	}
	utils.WriteSuccess(w, out)
}

// Open opens the native folder picker, saves the root, and starts a background scan.
func (h *CameraHandler) Open(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	cr, _ := h.settings.GetCameraRoot(r.Context(), userID)
	dir, err := service.ChooseReadableFolder(r.Context(), "Choose your camera or SD card folder", cr.Root)
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
		slog.Error("camera folder picker failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "couldn't open the folder picker", nil)
		return
	}
	label := service.CameraLabelFromPath(dir)
	if err := h.settings.SetCameraRoot(r.Context(), userID, dir, label); err != nil {
		slog.Error("save camera root failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to save the folder", nil)
		return
	}
	h.scanner.StartScan(userID, dir)
	utils.WriteSuccess(w, cameraOpenJSON{Root: dir, Label: label, Scan: "running", IndexedCount: 0})
}

// Refresh starts a background rescan of the saved root.
func (h *CameraHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil || !cr.HasRoot {
		utils.WriteError(w, http.StatusNotFound, "no camera folder chosen yet", nil)
		return
	}
	if !service.CameraRootAvailable(cr.Root) {
		utils.WriteError(w, http.StatusFailedDependency, "camera folder is not available — reconnect the card", nil)
		return
	}
	if !h.scanner.StartScan(userID, cr.Root) {
		utils.WriteError(w, http.StatusConflict, "a scan is already running", nil)
		return
	}
	utils.WriteSuccess(w, cameraOpenJSON{Root: cr.Root, Label: cr.Label, Scan: "running"})
}

// Items returns a page from the persisted camera index.
func (h *CameraHandler) Items(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil {
		slog.Error("get camera root failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load camera folder", nil)
		return
	}
	if !cr.HasRoot {
		utils.WriteError(w, http.StatusNotFound, "no camera folder chosen yet", nil)
		return
	}

	q := r.URL.Query()
	kind := q.Get("kind")
	sort := q.Get("sort")
	if sort != repository.CameraSortOldest {
		sort = repository.CameraSortLatest
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 24
	}
	if limit > 500 {
		limit = 500
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	// Legacy cursor mode if offset omitted and cursor set.
	cursor := q.Get("cursor")
	useOffset := q.Has("offset") || cursor == ""

	meta, _ := h.index.GetScanMeta(r.Context(), userID)
	if h.scanner.IsBusy(userID) {
		meta.Status = "running"
	}

	// If index empty and root available and not busy, kick a scan (reopen after restart).
	n, _ := h.index.CountAll(r.Context(), userID)
	if n == 0 && meta.Status != "running" && service.CameraRootAvailable(cr.Root) {
		h.scanner.StartScan(userID, cr.Root)
		meta.Status = "running"
	}

	scope := repository.ScopeMain
	if q.Get("folder") == repository.ScopeAirDrop {
		scope = repository.ScopeAirDrop
	}
	total, err := h.index.CountByKind(r.Context(), userID, kind, scope)
	if err != nil {
		slog.Error("count camera index failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load camera items", nil)
		return
	}

	var rows []repository.CameraIndexRow
	if useOffset {
		rows, err = h.index.ListOffset(r.Context(), userID, kind, offset, limit, sort, scope)
	} else {
		rows, err = h.index.ListPage(r.Context(), userID, kind, cursor, limit, scope)
	}
	if err != nil {
		slog.Error("list camera index failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load camera items", nil)
		return
	}
	copied, err := h.copies.Names(r.Context(), userID)
	if err != nil {
		slog.Error("copied basenames failed", "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load Local media", nil)
		return
	}

	dir := h.downloadDir(r, userID)
	sentNames := map[string]bool{}
	if h.tray != nil {
		sentNames, _ = h.tray.SentNames(r.Context(), userID)
	}
	items := make([]cameraItemJSON, 0, len(rows))
	for _, row := range rows {
		full := ""
		thumb := ""
		if row.Kind != "junk" {
			full = h.media.CameraURL(userID, row.RelPath).URL
			thumb = h.media.CameraThumbURL(userID, row.RelPath)
		}
		items = append(items, cameraItemJSON{
			RelPath:    row.RelPath,
			Name:       row.Name,
			Kind:       row.Kind,
			ByteSize:   row.ByteSize,
			MtimeNs:    row.MtimeNs,
			URL:        full,
			ThumbURL:   thumb,
			Copied:     copied[row.Name],
			Sent:       sentNames[row.Name],
			DurationMs: service.CameraVideoDurationMs(dir, userID, "", row.RelPath, row.ByteSize, row.MtimeNs, false),
		})
	}
	next := ""
	hasMore := offset+len(rows) < total
	if !useOffset {
		hasMore = len(rows) == limit
	}
	if len(rows) > 0 {
		next = rows[len(rows)-1].RelPath
	}
	utils.WriteSuccess(w, cameraItemsPageJSON{
		Root: cr.Root, Label: cr.Label, Items: items,
		Offset: offset, Limit: limit, Total: total,
		NextCursor: next, HasMore: hasMore,
		Scan: meta.Status, Truncated: meta.Truncated, TimedOut: meta.TimedOut,
	})
}

type cameraDeleteBody struct {
	RelPaths []string `json:"rel_paths"`
	// Trash moves the files to the Mac's Trash (can be put back) instead of deleting them for good.
	Trash bool `json:"trash"`
}

type cameraDeleteResultJSON struct {
	Deleted int      `json:"deleted"`
	Failed  []string `json:"failed,omitempty"`
}

// Delete removes selected files from the camera root on disk and from the index.
func (h *CameraHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil || !cr.HasRoot {
		utils.WriteError(w, http.StatusNotFound, "no camera folder chosen yet", nil)
		return
	}
	if !service.CameraRootAvailable(cr.Root) {
		utils.WriteError(w, http.StatusFailedDependency, "camera folder is not available — reconnect the card", nil)
		return
	}
	var body cameraDeleteBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid body", nil)
		return
	}
	if len(body.RelPaths) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "rel_paths required", nil)
		return
	}
	if len(body.RelPaths) > 500 {
		utils.WriteError(w, http.StatusBadRequest, "at most 500 files per delete", nil)
		return
	}

	okPaths := make([]string, 0, len(body.RelPaths))
	failed := make([]string, 0)
	var toTrash []string
	relOf := map[string]string{}
	seen := make(map[string]struct{}, len(body.RelPaths))
	for _, rel := range body.RelPaths {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" {
			continue
		}
		if _, dup := seen[rel]; dup {
			continue
		}
		seen[rel] = struct{}{}
		path, err := service.ResolveUnderRoot(cr.Root, rel)
		if err != nil {
			failed = append(failed, rel)
			continue
		}
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() {
			failed = append(failed, rel)
			continue
		}
		if body.Trash {
			toTrash = append(toTrash, path)
			relOf[path] = rel
			continue
		}
		if err := os.Remove(path); err != nil {
			slog.Warn("camera delete failed", "path", rel, "err", err)
			failed = append(failed, rel)
			continue
		}
		okPaths = append(okPaths, rel)
	}
	if len(toTrash) > 0 {
		rels := make([]string, 0, len(toTrash))
		for _, p := range toTrash {
			rels = append(rels, relOf[p])
		}
		indexed, _ := h.index.GetByRelPaths(r.Context(), userID, rels)
		info := map[string]repository.CameraIndexRow{}
		for _, row := range indexed {
			info[row.RelPath] = row
		}
		moved, notMoved, err := service.MoveToTrashTracked(toTrash)
		if err != nil {
			slog.Warn("camera move to trash failed", "err", err)
		}
		// Remember them so the Bin can list, put back, or delete them for good.
		for p, trashPath := range moved {
			rel := relOf[p]
			row, ok := info[rel]
			if !ok {
				row = repository.CameraIndexRow{RelPath: rel, Name: filepath.Base(p), Kind: service.MediaType(p)}
			}
			if h.trash != nil {
				if err := h.trash.Add(r.Context(), userID, repository.CardTrashItem{
					RelPath: rel, Name: row.Name, Kind: row.Kind, ByteSize: row.ByteSize, MtimeNs: row.MtimeNs,
					CardRoot: cr.Root, TrashPath: trashPath,
				}); err != nil {
					slog.Warn("record trashed card file failed", "file", rel, "err", err)
				}
			}
		}
		skip := map[string]bool{}
		for _, p := range notMoved {
			skip[p] = true
			failed = append(failed, relOf[p])
		}
		for _, p := range toTrash {
			if !skip[p] {
				okPaths = append(okPaths, relOf[p])
			}
		}
	}
	if h.tray != nil {
		for _, p := range okPaths {
			_ = h.tray.ForgetOrigin(r.Context(), userID, p)
		}
	}
	if len(okPaths) > 0 {
		if _, err := h.index.DeleteByRelPaths(r.Context(), userID, okPaths); err != nil {
			slog.Error("camera index delete failed", "user_id", userID, "err", err)
			utils.WriteError(w, http.StatusInternalServerError, "deleted from card but failed to update index", nil)
			return
		}
	}
	utils.WriteSuccess(w, cameraDeleteResultJSON{Deleted: len(okPaths), Failed: failed})
}

// AirDrop opens the Mac's AirDrop window for selected files on the camera card, read in place.
func (h *CameraHandler) AirDrop(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil || !cr.HasRoot {
		utils.WriteError(w, http.StatusNotFound, "no camera folder chosen yet", nil)
		return
	}
	if !service.CameraRootAvailable(cr.Root) {
		utils.WriteError(w, http.StatusFailedDependency, "camera folder is not available — reconnect the card", nil)
		return
	}
	var body cameraDeleteBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || len(body.RelPaths) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "rel_paths required", nil)
		return
	}
	if len(body.RelPaths) > 500 {
		utils.WriteError(w, http.StatusBadRequest, "at most 500 files per share", nil)
		return
	}
	paths := make([]string, 0, len(body.RelPaths))
	for _, rel := range body.RelPaths {
		path, err := service.ResolveUnderRoot(cr.Root, filepath.ToSlash(strings.TrimSpace(rel)))
		if err != nil {
			continue
		}
		if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
			paths = append(paths, path)
		}
	}
	writeAirDrop(w, paths)
}

func writeAirDrop(w http.ResponseWriter, paths []string) {
	if len(paths) == 0 {
		utils.WriteError(w, http.StatusNotFound, "selected files are missing on disk", nil)
		return
	}
	if err := service.OpenAirDrop(paths); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrAirDropUnavailable) {
			status = http.StatusFailedDependency
		}
		utils.WriteError(w, status, err.Error(), nil)
		return
	}
	utils.WriteSuccess(w, map[string]int{"files": len(paths)})
}

// Eject safely ejects the card that holds the camera folder. The folder and index are kept,
// so plugging the card back in shows the same files again.
func (h *CameraHandler) Eject(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil || !cr.HasRoot {
		utils.WriteError(w, http.StatusNotFound, "no camera folder chosen yet", nil)
		return
	}
	if !service.CameraRootAvailable(cr.Root) {
		utils.WriteError(w, http.StatusConflict, "the card is already disconnected", nil)
		return
	}
	if h.scanner.IsBusy(userID) {
		utils.WriteError(w, http.StatusConflict, "wait for the scan to finish, then eject", nil)
		return
	}
	mount := service.VolumeMountPoint(cr.Root)
	if err := service.EjectVolume(r.Context(), mount); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrNotRemovable) {
			status = http.StatusBadRequest
		} else if errors.Is(err, service.ErrVolumeBusy) {
			status = http.StatusConflict
		}
		slog.Warn("eject card failed", "mount", mount, "err", err)
		utils.WriteError(w, status, err.Error(), nil)
		return
	}
	utils.WriteSuccess(w, map[string]string{"ejected": filepath.Base(mount)})
}

// ClearRoot forgets the saved camera path and index.
func (h *CameraHandler) ClearRoot(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return
	}
	if err := h.settings.ClearCameraRoot(r.Context(), userID); err != nil {
		slog.Error("clear camera root failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not forget the camera folder", nil)
		return
	}
	_ = h.index.Clear(r.Context(), userID)
	_ = h.index.SetScanMeta(r.Context(), userID, repository.CameraScanMeta{Status: "idle"})
	utils.WriteSuccess(w, map[string]string{"ok": "true"})
}

// Media serves a signed file under the user's camera root.
func (h *CameraHandler) Media(w http.ResponseWriter, r *http.Request) {
	userID, err1 := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	file := filepath.ToSlash(strings.TrimPrefix(chi.URLParam(r, "*"), "/"))
	q := r.URL.Query()
	if err1 != nil || file == "" || !h.media.VerifyCamera(userID, file, q.Get("exp"), q.Get("sig")) {
		http.Error(w, "link expired or invalid", http.StatusForbidden)
		return
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil || !cr.HasRoot {
		http.NotFound(w, r)
		return
	}
	path, err := service.ResolveUnderRoot(cr.Root, file)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	serveCardFile(w, r, path)
}

// Thumb serves a JPEG thumbnail (generated + cached) for a camera file.
func (h *CameraHandler) Thumb(w http.ResponseWriter, r *http.Request) {
	userID, err1 := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	file := filepath.ToSlash(strings.TrimPrefix(chi.URLParam(r, "*"), "/"))
	q := r.URL.Query()
	if err1 != nil || file == "" || !h.media.VerifyCameraThumb(userID, file, q.Get("exp"), q.Get("sig")) {
		http.Error(w, "link expired or invalid", http.StatusForbidden)
		return
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil || !cr.HasRoot {
		http.NotFound(w, r)
		return
	}
	src, err := service.ResolveUnderRoot(cr.Root, file)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	fi, err := os.Stat(src)
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	dir := h.downloadDir(r, userID)
	thumb, err := service.EnsureCameraThumbSingleFlight(
		r.Context(), dir, userID, src, file, fi.Size(), fi.ModTime().UnixNano(),
	)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Warn("camera thumb failed", "file", file, "err", err)
		// Fall back to original for images so the tile is not blank.
		if service.MediaType(file) == "image" {
			serveCardFile(w, r, src)
			return
		}
		http.Error(w, "thumbnail unavailable", http.StatusNotFound)
		return
	}
	if ms := service.CameraVideoDurationMs(dir, userID, src, file, fi.Size(), fi.ModTime().UnixNano(), true); ms > 0 {
		w.Header().Set("X-Duration-Ms", strconv.FormatInt(ms, 10))
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	serveFile(w, r, thumb)
}

// serveCardFile serves a file straight from the camera card, tracked so Eject can cut it off.
func serveCardFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	untrack := service.TrackCardFile(f)
	defer untrack()
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(path), fi.ModTime(), f)
}

func serveFile(w http.ResponseWriter, r *http.Request, path string) {
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
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "private, max-age=3600")
	}
	http.ServeContent(w, r, filepath.Base(path), fi.ModTime(), f)
}
