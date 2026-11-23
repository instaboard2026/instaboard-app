package service

import (
	"context"
	"log/slog"
	"sync"

	"instaboard/internal/repository"
)

// CameraScanner runs background SD walks and streams into tbl_camera_index.
type CameraScanner struct {
	index *repository.CameraIndexRepo
	mu    sync.Mutex
	busy  map[int64]bool
}

// NewCameraScanner creates a CameraScanner.
func NewCameraScanner(index *repository.CameraIndexRepo) *CameraScanner {
	return &CameraScanner{index: index, busy: map[int64]bool{}}
}

// StartScan clears the index and walks root in the background. No-op if already running.
func (s *CameraScanner) StartScan(userID int64, root string) bool {
	s.mu.Lock()
	if s.busy[userID] {
		s.mu.Unlock()
		return false
	}
	s.busy[userID] = true
	s.mu.Unlock()

	ctx := context.Background()
	_ = s.index.SetScanMeta(ctx, userID, repository.CameraScanMeta{Status: "running"})
	_ = s.index.Clear(ctx, userID)

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.busy, userID)
			s.mu.Unlock()
		}()

		res, err := ScanCameraMediaBatched(root, 250, func(batch []CameraFile) error {
			rows := make([]repository.CameraIndexRow, len(batch))
			for i, f := range batch {
				rows[i] = repository.CameraIndexRow{
					RelPath: f.RelPath, Name: f.Name, Kind: f.Kind,
					ByteSize: f.ByteSize, MtimeNs: f.MtimeNs,
				}
			}
			if err := s.index.InsertBatch(ctx, userID, rows); err != nil {
				return err
			}
			n, _ := s.index.CountAll(ctx, userID)
			_ = s.index.SetScanMeta(ctx, userID, repository.CameraScanMeta{
				Status: "running", IndexedCount: n, Truncated: false, TimedOut: false,
			})
			return nil
		})
		if err != nil {
			slog.Error("camera background scan failed", "user_id", userID, "err", err)
			n, _ := s.index.CountAll(ctx, userID)
			_ = s.index.SetScanMeta(ctx, userID, repository.CameraScanMeta{
				Status: "error", IndexedCount: n, ErrorMessage: err.Error(),
			})
			return
		}
		_ = s.index.SetScanMeta(ctx, userID, repository.CameraScanMeta{
			Status:       "done",
			IndexedCount: res.Count,
			Truncated:    res.Truncated,
			TimedOut:     res.TimedOut,
		})
	}()
	return true
}

// IsBusy reports whether a scan is in flight for the user.
func (s *CameraScanner) IsBusy(userID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy[userID]
}
