package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"

	"instaboard/internal/repository"
)

// MigrateLocalMediaToFolders moves every Local media folder into a regular sidebar folder of the same
// name (cards in that folder, files moved, camera "copied" marks kept), then removes the Local media
// folder. Safe to run on every start: once nothing is left it does nothing.
func (h *DumpHandler) MigrateLocalMediaToFolders(ctx context.Context, folders *repository.FolderRepo) {
	if h.local == nil {
		return
	}
	users, err := h.local.UserIDs(ctx)
	if err != nil {
		slog.Error("local media migration: list users failed", "err", err)
		return
	}
	for _, userID := range users {
		dir, err := h.settings.DownloadDir(ctx, userID)
		if err != nil || dir == "" {
			dir = h.defaultDir
		}
		lmDir := filepath.Join(dir, "local-media", strconv.FormatInt(userID, 10))
		lmFolders, err := h.local.ListFolders(ctx, userID)
		if err != nil {
			slog.Error("local media migration: list folders failed", "user_id", userID, "err", err)
			continue
		}
		for _, lf := range lmFolders {
			items, err := h.local.ListItems(ctx, userID, lf.ID)
			if err != nil {
				slog.Error("local media migration: list items failed", "folder", lf.ID, "err", err)
				continue
			}
			if len(items) > 0 {
				target, err := createFolderFreeName(ctx, folders, userID, lf.Name)
				if err != nil {
					slog.Error("local media migration: create folder failed", "folder", lf.Name, "err", err)
					continue
				}
				for _, it := range items {
					base := filepath.Base(it.StoredName)
					if base != it.StoredName {
						continue
					}
					if _, err := h.saveLocalFile(ctx, userID, dir, &target, filepath.Join(lmDir, base), it.OriginalName, "local-media", true); err != nil {
						slog.Error("local media migration: move file failed", "item", it.ID, "err", err)
						continue
					}
					if _, err := h.local.DeleteItem(ctx, userID, it.ID); err != nil {
						slog.Warn("local media migration: item row remains", "item", it.ID, "err", err)
					}
				}
				slog.Info("moved Local media folder into Folders", "user_id", userID, "folder", lf.Name, "files", len(items))
			}
			// Only remove the Local media folder once it is empty (failed files stay put for the next start).
			if left, err := h.local.ListItems(ctx, userID, lf.ID); err == nil && len(left) == 0 {
				if _, err := h.local.DeleteFolder(ctx, userID, lf.ID); err != nil {
					slog.Warn("local media migration: delete folder failed", "folder", lf.ID, "err", err)
				}
			}
		}
	}
}

// createFolderFreeName creates a folder called name, or "name (2)", "name (3)"… if taken.
func createFolderFreeName(ctx context.Context, folders *repository.FolderRepo, userID int64, name string) (int64, error) {
	for i := 1; i <= 50; i++ {
		try := name
		if i > 1 {
			try = fmt.Sprintf("%s (%d)", name, i)
		}
		f, err := folders.Create(ctx, userID, try, nil)
		if errors.Is(err, repository.ErrFolderNameTaken) || errors.Is(err, repository.ErrFolderNameInvalid) {
			continue
		}
		if err != nil {
			return 0, err
		}
		return f.ID, nil
	}
	return 0, repository.ErrFolderNameTaken
}
