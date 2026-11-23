package service

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// MaxCameraScanFiles caps how many media files one walk indexes.
	MaxCameraScanFiles = 50000
	// MaxCameraScanDuration stops a walk that takes too long.
	MaxCameraScanDuration = 3 * time.Minute
)

// CameraFile is one image/video/junk file found under a camera root (flattened).
type CameraFile struct {
	RelPath  string `json:"rel_path"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // image | video | junk
	ByteSize int64  `json:"byte_size"`
	MtimeNs  int64  `json:"mtime_ns"`
}

// CameraScanResult is the outcome of walking a camera folder.
type CameraScanResult struct {
	Items     []CameraFile `json:"items"`
	Count     int          `json:"count"`
	Truncated bool         `json:"truncated"`
	TimedOut  bool         `json:"timed_out"`
}

var cameraExtKind = map[string]string{
	".jpg":  "image",
	".jpeg": "image",
	".png":  "image",
	".gif":  "image",
	".webp": "image",
	".heic": "image",
	".heif": "image",
	".tif":  "image",
	".tiff": "image",
	".bmp":  "image",
	".mp4":  "video",
	".mov":  "video",
	".m4v":  "video",
	".webm": "video",
	".avi":  "video",
	".mkv":  "video",
	".mts":  "video",
	".m2ts": "video",
	".3gp":  "video",
}

// CameraImageBrowserSafe reports whether Chromium can display the file without transcoding.
func CameraImageBrowserSafe(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

// CameraLabelFromPath returns a short label for reconnect UI (volume name when under /Volumes).
func CameraLabelFromPath(root string) string {
	clean := filepath.Clean(root)
	parts := strings.Split(clean, string(os.PathSeparator))
	for i, p := range parts {
		if p == "Volumes" && i+1 < len(parts) && parts[i+1] != "" {
			return parts[i+1]
		}
	}
	base := filepath.Base(clean)
	if base == "" || base == "." || base == string(os.PathSeparator) {
		return clean
	}
	return base
}

// CameraRootAvailable reports whether root exists and is a readable directory.
func CameraRootAvailable(root string) bool {
	if root == "" || !filepath.IsAbs(root) {
		return false
	}
	fi, err := os.Stat(root)
	return err == nil && fi.IsDir()
}

// ScanCameraMedia recursively finds images/videos/junk under root.
func ScanCameraMedia(root string) (CameraScanResult, error) {
	var all []CameraFile
	res, err := ScanCameraMediaBatched(root, 500, func(batch []CameraFile) error {
		all = append(all, batch...)
		return nil
	})
	if err != nil {
		return CameraScanResult{}, err
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].RelPath < all[j].RelPath
	})
	res.Items = all
	res.Count = len(all)
	return res, nil
}

// ScanCameraMediaBatched walks the tree and invokes onBatch for each chunk (for streaming into SQLite).
func ScanCameraMediaBatched(root string, batchSize int, onBatch func([]CameraFile) error) (CameraScanResult, error) {
	root = filepath.Clean(root)
	if !CameraRootAvailable(root) {
		return CameraScanResult{}, os.ErrNotExist
	}
	if batchSize <= 0 {
		batchSize = 250
	}
	deadline := time.Now().Add(MaxCameraScanDuration)
	var out CameraScanResult
	buf := make([]CameraFile, 0, batchSize)

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		cp := append([]CameraFile(nil), buf...)
		if err := onBatch(cp); err != nil {
			return err
		}
		out.Count += len(buf)
		buf = buf[:0]
		return nil
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if time.Now().After(deadline) {
			out.TimedOut = true
			return fs.SkipAll
		}
		if d.IsDir() {
			name := d.Name()
			if name == "." || name == ".." {
				return nil
			}
			lower := strings.ToLower(name)
			if strings.HasPrefix(name, ".") || lower == "system volume information" || lower == "$recycle.bin" {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		// Skip ordinary dotfiles (.DS_Store); AppleDouble (._*) is indexed as junk below.
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "._") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		kind := ""
		switch {
		case ext == ".xml":
			kind = "junk"
		case strings.HasPrefix(name, "._"):
			if _, ok := cameraExtKind[ext]; !ok {
				return nil
			}
			kind = "junk"
		default:
			var ok bool
			kind, ok = cameraExtKind[ext]
			if !ok {
				return nil
			}
		}
		if out.Count+len(buf) >= MaxCameraScanFiles {
			out.Truncated = true
			return fs.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Tiny "images" are usually stubs / junk, not real photos.
		if kind == "image" && info.Size() < 1024 {
			kind = "junk"
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		buf = append(buf, CameraFile{
			RelPath:  filepath.ToSlash(rel),
			Name:     name,
			Kind:     kind,
			ByteSize: info.Size(),
			MtimeNs:  info.ModTime().UnixNano(),
		})
		if len(buf) >= batchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return CameraScanResult{}, err
	}
	if err := flush(); err != nil {
		return CameraScanResult{}, err
	}
	return out, nil
}

// ResolveUnderRoot joins root + rel and ensures the result stays under root (no escape).
func ResolveUnderRoot(root, rel string) (string, error) {
	root = filepath.Clean(root)
	rel = filepath.ToSlash(rel)
	if rel == "" || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return "", os.ErrInvalid
	}
	joined := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))

	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	joinedReal, err := filepath.EvalSymlinks(joined)
	if err != nil {
		joinedReal = joined
	}
	sep := string(os.PathSeparator)
	if joinedReal != rootReal && !strings.HasPrefix(joinedReal, rootReal+sep) {
		return "", os.ErrPermission
	}
	return joined, nil
}
