package service

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// DiskSpace is free / total capacity for the volume containing path.
type DiskSpace struct {
	FreeBytes  uint64 `json:"free_bytes"`
	TotalBytes uint64 `json:"total_bytes"`
}

// DiskSpaceForPath reports filesystem capacity for the volume of path (macOS / Unix).
func DiskSpaceForPath(path string) (DiskSpace, error) {
	path = filepath.Clean(path)
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return DiskSpace{}, err
	}
	// Prefer Bavail (free for non-root); fall back to Bfree.
	bsize := uint64(st.Bsize)
	if bsize == 0 {
		bsize = 512
	}
	freeBlocks := st.Bavail
	if freeBlocks == 0 {
		freeBlocks = st.Bfree
	}
	return DiskSpace{
		FreeBytes:  uint64(freeBlocks) * bsize,
		TotalBytes: st.Blocks * bsize,
	}, nil
}
