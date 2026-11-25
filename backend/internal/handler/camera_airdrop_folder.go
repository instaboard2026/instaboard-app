package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"instaboard/internal/middleware"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/pkg/utils"
)

// AirDrop folder: a real AIRDROP/ folder at the top of the card. Files dragged onto AirDrop are
// moved there (instant, same card); from its page they can be AirDropped, moved back or deleted.

var airdropSending sync.Map // userID -> bool

// SetAirDropTray wires storage for AIRDROP/ origins and the "Sent" marks.
func (h *CameraHandler) SetAirDropTray(t *repository.AirDropTrayRepo) { h.tray = t }

type airdropRequest struct {
	RelPaths []string `json:"rel_paths"`
}

type airdropResultJSON struct {
	Moved   int      `json:"moved"`
	Already int      `json:"already"`
	Failed  []string `json:"failed,omitempty"`
}

func isInAirDrop(rel string) bool {
	return strings.HasPrefix(strings.ToUpper(rel), repository.AirDropFolder+"/")
}

// cardForUser returns the connected camera root, writing an error if there is none.
func (h *CameraHandler) cardForUser(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == 0 {
		utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
		return 0, "", false
	}
	cr, err := h.settings.GetCameraRoot(r.Context(), userID)
	if err != nil || !cr.HasRoot || !service.CameraRootAvailable(cr.Root) {
		utils.WriteError(w, http.StatusFailedDependency, "plug the card in first", nil)
		return 0, "", false
	}
	return userID, cr.Root, true
}

func decodeAirDropRequest(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var body airdropRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || len(body.RelPaths) > 500 {
		utils.WriteError(w, http.StatusBadRequest, "rel_paths required (max 500)", nil)
		return nil, false
	}
	return cleanRelPaths(body.RelPaths), true
}

// AddToAirDrop moves files into AIRDROP/ on the card.
func (h *CameraHandler) AddToAirDrop(w http.ResponseWriter, r *http.Request) {
	userID, root, ok := h.cardForUser(w, r)
	if !ok {
		return
	}
	rels, ok := decodeAirDropRequest(w, r)
	if !ok || len(rels) == 0 {
		if ok {
			utils.WriteError(w, http.StatusBadRequest, "rel_paths required", nil)
		}
		return
	}
	out := airdropResultJSON{}
	for _, rel := range rels {
		if isInAirDrop(rel) {
			out.Already++
			continue
		}
		newRel, err := h.moveOnCard(r.Context(), userID, root, rel, repository.AirDropFolder)
		if err != nil {
			slog.Warn("move into AirDrop failed", "file", rel, "err", err)
			out.Failed = append(out.Failed, rel)
			continue
		}
		if err := h.tray.RecordOrigin(r.Context(), userID, newRel, rel); err != nil {
			slog.Warn("record AirDrop origin failed", "file", newRel, "err", err)
		}
		out.Moved++
	}
	utils.WriteSuccess(w, out)
}

// MoveBackFromAirDrop returns files from AIRDROP/ to where they were on the card.
func (h *CameraHandler) MoveBackFromAirDrop(w http.ResponseWriter, r *http.Request) {
	userID, root, ok := h.cardForUser(w, r)
	if !ok {
		return
	}
	rels, ok := decodeAirDropRequest(w, r)
	if !ok {
		return
	}
	out := airdropResultJSON{}
	for _, rel := range rels {
		if !isInAirDrop(rel) {
			out.Already++
			continue
		}
		origin, _ := h.tray.Origin(r.Context(), userID, rel)
		if origin == "" {
			out.Failed = append(out.Failed, rel)
			continue
		}
		if _, err := h.moveOnCard(r.Context(), userID, root, rel, path.Dir(origin)); err != nil {
			slog.Warn("move back from AirDrop failed", "file", rel, "err", err)
			out.Failed = append(out.Failed, rel)
			continue
		}
		_ = h.tray.ForgetOrigin(r.Context(), userID, rel)
		out.Moved++
	}
	utils.WriteSuccess(w, out)
}

// SendAirDrop opens one AirDrop window with the given files (or everything in AIRDROP/ when none
// are given). When macOS reports them shared they get the "Sent" tag; files stay in the folder.
func (h *CameraHandler) SendAirDrop(w http.ResponseWriter, r *http.Request) {
	userID, root, ok := h.cardForUser(w, r)
	if !ok {
		return
	}
	if v, ok := airdropSending.Load(userID); ok && v.(bool) {
		utils.WriteError(w, http.StatusConflict, "the AirDrop window is already open", nil)
		return
	}
	rels, ok := decodeAirDropRequest(w, r)
	if !ok {
		return
	}
	if len(rels) == 0 {
		rows, err := h.index.ListOffset(r.Context(), userID, "all", 0, 500, repository.CameraSortOldest, repository.ScopeAirDrop)
		if err != nil {
			utils.WriteError(w, http.StatusInternalServerError, "could not read the AirDrop folder", nil)
			return
		}
		for _, row := range rows {
			rels = append(rels, row.RelPath)
		}
	}
	paths := make([]string, 0, len(rels))
	names := make([]string, 0, len(rels))
	for _, rel := range rels {
		p, err := service.ResolveUnderRoot(root, rel)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			paths = append(paths, p)
			names = append(names, filepath.Base(p))
		}
	}
	if len(paths) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "the AirDrop folder is empty", nil)
		return
	}
	airdropSending.Store(userID, true)
	err := service.OpenAirDropThen(paths, func(sent bool) {
		defer airdropSending.Store(userID, false)
		if sent {
			if err := h.tray.MarkSent(context.Background(), userID, names); err != nil {
				slog.Warn("mark airdropped failed", "err", err)
			}
		}
	})
	if err != nil {
		airdropSending.Store(userID, false)
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrAirDropUnavailable) {
			status = http.StatusFailedDependency
		}
		utils.WriteError(w, status, err.Error(), nil)
		return
	}
	utils.WriteSuccess(w, map[string]int{"files": len(paths)})
}

// moveOnCard renames a card file into destDir (relative to the card root, created if needed),
// picking "name (2).ext" if the name is taken, and updates the index. Returns the new rel path.
func (h *CameraHandler) moveOnCard(ctx context.Context, userID int64, root, rel, destDir string) (string, error) {
	src, err := service.ResolveUnderRoot(root, rel)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(src)
	if err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("file not found")
	}
	dir, err := service.ResolveUnderRoot(root, destDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	dest := filepath.Join(dir, base)
	for i := 2; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		if i > 99 {
			return "", errors.New("too many files with that name")
		}
		dest = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	if err := os.Rename(src, dest); err != nil {
		return "", err
	}
	newRel := filepath.ToSlash(strings.TrimPrefix(dest, filepath.Clean(root)+string(filepath.Separator)))
	if _, err := h.index.DeleteByRelPaths(ctx, userID, []string{rel}); err != nil {
		slog.Warn("index update after move failed", "file", rel, "err", err)
	}
	kind := service.MediaType(dest)
	if kind != "image" && kind != "video" {
		kind = "junk"
	}
	if err := h.index.InsertBatch(ctx, userID, []repository.CameraIndexRow{{
		RelPath: newRel, Name: filepath.Base(dest), Kind: kind, ByteSize: fi.Size(), MtimeNs: fi.ModTime().UnixNano(),
	}}); err != nil {
		slog.Warn("index insert after move failed", "file", newRel, "err", err)
	}
	return newRel, nil
}

func cleanRelPaths(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p = filepath.ToSlash(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
