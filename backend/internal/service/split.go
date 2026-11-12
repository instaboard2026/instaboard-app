package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"instaboard/internal/models"
)

const (
	// Default scdet threshold — Instagram-style hard cuts often score below 10.
	defaultDetectThreshold = 5.0
	// Companion select-filter scene score (0–1); merged with scdet for better recall.
	defaultSceneScore = 0.2
	minClipMs         = 400
	masterFileName    = "master.mp4"
)

var (
	scdetTimeRe = regexp.MustCompile(`(?:lavfi\.scd\.time|pts_time)[=:]([0-9]+(?:\.[0-9]+)?)`)
	durationRe  = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
)

// SplitService copies masters into a workspace and runs ffmpeg cut detection + segmenting.
type SplitService struct {
	downloader *Downloader
}

// NewSplitService creates a SplitService.
func NewSplitService(d *Downloader) *SplitService {
	return &SplitService{downloader: d}
}

// ProjectDir is {downloadDir}/split/{userID}/{projectID}.
func (s *SplitService) ProjectDir(downloadDir string, userID int64, projectID uint64) string {
	return filepath.Join(downloadDir, "split", strconv.FormatInt(userID, 10), strconv.FormatUint(projectID, 10))
}

// MasterPath returns the absolute path of the project's master copy.
func (s *SplitService) MasterPath(downloadDir string, userID int64, projectID uint64) string {
	return filepath.Join(s.ProjectDir(downloadDir, userID, projectID), masterFileName)
}

// ClipPath returns the absolute path of a clip file under clips/.
func (s *SplitService) ClipPath(downloadDir string, userID int64, projectID uint64, fileName string) string {
	if fileName == "" || fileName != filepath.Base(fileName) || strings.HasPrefix(fileName, ".") {
		return ""
	}
	return filepath.Join(s.ProjectDir(downloadDir, userID, projectID), "clips", fileName)
}

// MediaPath resolves a relative media path (master.mp4 or clips/N.mp4) under the project dir.
func (s *SplitService) MediaPath(downloadDir string, userID int64, projectID uint64, rel string) string {
	rel = filepath.ToSlash(rel)
	switch {
	case rel == masterFileName:
		return s.MasterPath(downloadDir, userID, projectID)
	case strings.HasPrefix(rel, "clips/"):
		return s.ClipPath(downloadDir, userID, projectID, strings.TrimPrefix(rel, "clips/"))
	default:
		return ""
	}
}

// CopyMasterFromSave copies a dump media file into the project as master.mp4. Does not modify the save.
func (s *SplitService) CopyMasterFromSave(downloadDir string, userID int64, projectID, saveID uint64, file string) (durationMs int64, err error) {
	src := s.downloader.MediaPath(downloadDir, saveID, file)
	if src == "" {
		return 0, fmt.Errorf("invalid media file")
	}
	if _, err := os.Stat(src); err != nil {
		return 0, fmt.Errorf("source video not found")
	}
	return s.writeMaster(downloadDir, userID, projectID, func(dst string) error {
		return copyFile(src, dst)
	})
}

// StoreMasterUpload writes an uploaded video as master.mp4.
func (s *SplitService) StoreMasterUpload(downloadDir string, userID int64, projectID uint64, r io.Reader) (durationMs int64, err error) {
	return s.writeMaster(downloadDir, userID, projectID, func(dst string) error {
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, r)
		return err
	})
}

func (s *SplitService) writeMaster(downloadDir string, userID int64, projectID uint64, write func(dst string) error) (int64, error) {
	dir := s.ProjectDir(downloadDir, userID, projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	dst := filepath.Join(dir, masterFileName)
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	if err := write(tmp); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	ms, err := probeDurationMs(dst)
	if err != nil {
		return 0, err
	}
	return ms, nil
}

// RemoveProjectDir deletes the on-disk project folder.
func (s *SplitService) RemoveProjectDir(downloadDir string, userID int64, projectID uint64) error {
	return os.RemoveAll(s.ProjectDir(downloadDir, userID, projectID))
}

// ClearClipsDir removes previous clip files before a re-cut.
func (s *SplitService) ClearClipsDir(downloadDir string, userID int64, projectID uint64) error {
	dir := filepath.Join(s.ProjectDir(downloadDir, userID, projectID), "clips")
	_ = os.RemoveAll(dir)
	return os.MkdirAll(dir, 0o755)
}

// DetectAndSplit finds hard cuts and writes clip files. Returns clip rows (without DB ids).
func (s *SplitService) DetectAndSplit(ctx context.Context, downloadDir string, userID int64, projectID uint64, durationMs int64, threshold float64) ([]models.SplitClip, error) {
	if threshold <= 0 {
		threshold = defaultDetectThreshold
	}
	master := s.MasterPath(downloadDir, userID, projectID)
	if _, err := os.Stat(master); err != nil {
		return nil, fmt.Errorf("master video missing")
	}
	if durationMs <= 0 {
		ms, err := probeDurationMs(master)
		if err != nil {
			return nil, err
		}
		durationMs = ms
	}

	cuts, err := detectSceneCuts(ctx, master, threshold)
	if err != nil {
		return nil, err
	}
	ranges := buildClipRanges(durationMs, cuts, minClipMs)
	if len(ranges) == 0 {
		return nil, nil
	}

	if err := s.ClearClipsDir(downloadDir, userID, projectID); err != nil {
		return nil, err
	}

	out := make([]models.SplitClip, 0, len(ranges))
	for i, rg := range ranges {
		name := fmt.Sprintf("%d.mp4", i+1)
		dst := s.ClipPath(downloadDir, userID, projectID, name)
		if err := exportClip(ctx, master, dst, rg.startMs, rg.endMs); err != nil {
			return nil, fmt.Errorf("export clip %d: %w", i+1, err)
		}
		out = append(out, models.SplitClip{
			ProjectID: projectID,
			Position:  i + 1,
			StartMs:   rg.startMs,
			EndMs:     rg.endMs,
			FileName:  name,
		})
	}
	return out, nil
}

// FFmpegAvailable reports whether ffmpeg can be resolved.
func FFmpegAvailable() bool {
	return toolPath("ffmpeg") != ""
}

type clipRange struct {
	startMs, endMs int64
}

func buildClipRanges(durationMs int64, cutSec []float64, minMs int64) []clipRange {
	if durationMs <= 0 {
		return nil
	}
	edges := []int64{0}
	for _, sec := range cutSec {
		ms := int64(sec*1000 + 0.5)
		if ms < minMs || ms > durationMs-minMs {
			continue
		}
		if ms-edges[len(edges)-1] < minMs {
			continue
		}
		edges = append(edges, ms)
	}
	if durationMs-edges[len(edges)-1] < minMs && len(edges) > 1 {
		edges = edges[:len(edges)-1]
	}
	if len(edges) == 1 {
		// No usable cuts — caller shows master only.
		return nil
	}
	edges = append(edges, durationMs)
	out := make([]clipRange, 0, len(edges)-1)
	for i := 0; i < len(edges)-1; i++ {
		if edges[i+1] <= edges[i] {
			continue
		}
		out = append(out, clipRange{startMs: edges[i], endMs: edges[i+1]})
	}
	return out
}

func detectSceneCuts(ctx context.Context, master string, threshold float64) ([]float64, error) {
	bin := toolPath("ffmpeg")
	if bin == "" {
		return nil, fmt.Errorf("ffmpeg is not installed")
	}
	// scdet (0–100) plus select scene (0–1). Reels often need both — threshold 10 alone
	// misses softer hard-cuts that still look like edits to the eye.
	sceneScore := defaultSceneScore
	if threshold >= 10 {
		sceneScore = 0.3
	} else if threshold <= 3 {
		sceneScore = 0.15
	}

	scdetFilter := fmt.Sprintf(
		"scale=640:-2:flags=area,scdet=threshold=%g,metadata=mode=print:key=lavfi.scd.time",
		threshold,
	)
	selectFilter := fmt.Sprintf(
		"scale=640:-2:flags=area,select='gt(scene\\,%g)',showinfo",
		sceneScore,
	)

	seen := map[int64]struct{}{}
	var cuts []float64
	addFromStderr := func(stderr string) {
		for _, line := range strings.Split(stderr, "\n") {
			m := scdetTimeRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			sec, err := strconv.ParseFloat(m[1], 64)
			if err != nil || sec <= 0 {
				continue
			}
			key := int64(sec*1000 + 0.5)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			cuts = append(cuts, sec)
		}
	}

	for _, filter := range []string{scdetFilter, selectFilter} {
		cmd := exec.CommandContext(ctx, bin,
			"-hide_banner", "-nostats",
			"-i", master,
			"-an",
			"-vf", filter,
			"-f", "null", "-",
		)
		cmd.Env = append(os.Environ(), "PATH="+pathWithManagedBin())
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		_ = cmd.Run() // null mux often exits non-zero; parse stderr anyway
		addFromStderr(stderr.String())
	}

	sort.Float64s(cuts)
	return cuts, nil
}

func exportClip(ctx context.Context, master, dst string, startMs, endMs int64) error {
	bin := toolPath("ffmpeg")
	if bin == "" {
		return fmt.Errorf("ffmpeg is not installed")
	}
	start := formatTs(startMs)
	dur := formatTs(endMs - startMs)
	cmd := exec.CommandContext(ctx, bin,
		"-hide_banner", "-y",
		"-ss", start,
		"-i", master,
		"-t", dur,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		dst,
	)
	cmd.Env = append(os.Environ(), "PATH="+pathWithManagedBin())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", truncate(msg, 400))
	}
	return nil
}

func probeDurationMs(path string) (int64, error) {
	bin := toolPath("ffmpeg")
	if bin == "" {
		// Fall back to ffprobe if present, else error.
		if p := toolPath("ffprobe"); p != "" {
			return ffprobeDurationMs(p, path)
		}
		return 0, fmt.Errorf("ffmpeg is not installed")
	}
	// Prefer ffprobe when available for clean output.
	if p := toolPath("ffprobe"); p != "" {
		return ffprobeDurationMs(p, path)
	}
	cmd := exec.Command(bin, "-hide_banner", "-i", path)
	cmd.Env = append(os.Environ(), "PATH="+pathWithManagedBin())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	m := durationRe.FindStringSubmatch(stderr.String())
	if m == nil {
		return 0, fmt.Errorf("could not read video duration")
	}
	h, _ := strconv.ParseInt(m[1], 10, 64)
	min, _ := strconv.ParseInt(m[2], 10, 64)
	sec, _ := strconv.ParseFloat(m[3], 64)
	ms := int64((float64(h*3600+min*60)+sec)*1000 + 0.5)
	if ms <= 0 {
		return 0, fmt.Errorf("invalid video duration")
	}
	return ms, nil
}

func ffprobeDurationMs(bin, path string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	cmd.Env = append(os.Environ(), "PATH="+pathWithManagedBin())
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("could not read video duration")
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || sec <= 0 {
		return 0, fmt.Errorf("could not read video duration")
	}
	return int64(sec*1000 + 0.5), nil
}

func formatTs(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	sec := float64(ms) / 1000.0
	return strconv.FormatFloat(sec, 'f', 3, 64)
}
