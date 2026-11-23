package service

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// CopyJob tracks a background Local media copy.
type CopyJob struct {
	ID         string    `json:"id"`
	UserID     int64     `json:"-"`
	FolderID   int64     `json:"folder_id"`
	Status     string    `json:"status"` // pending | running | done | error
	Total      int       `json:"total"`
	Done       int       `json:"done"`
	Copied     int       `json:"copied"`
	Skipped    int       `json:"skipped"`
	Failed     int       `json:"failed"`
	BytesDone  int64     `json:"bytes_done"`
	CopiedNames  []string      `json:"copied_names"`
	SkippedPaths []string      `json:"skipped_paths"`
	Errors       []CopyJobError `json:"errors"`
	Message    string    `json:"message,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	mu         sync.Mutex
}

// CopyJobError is one failed path in a job.
type CopyJobError struct {
	RelPath string `json:"rel_path"`
	Error   string `json:"error"`
}

// CopyJobStore holds in-memory copy jobs (local single-user app).
type CopyJobStore struct {
	mu   sync.Mutex
	jobs map[string]*CopyJob
}

// NewCopyJobStore creates an empty store.
func NewCopyJobStore() *CopyJobStore {
	return &CopyJobStore{jobs: map[string]*CopyJob{}}
}

// Create registers a new pending job.
func (s *CopyJobStore) Create(userID, folderID int64, total int) *CopyJob {
	now := time.Now()
	j := &CopyJob{
		ID:        newJobID(),
		UserID:    userID,
		FolderID:  folderID,
		Status:    "pending",
		Total:     total,
		CreatedAt: now,
		UpdatedAt: now,
		CopiedNames: []string{},
		SkippedPaths: []string{},
		Errors: []CopyJobError{},
	}
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.mu.Unlock()
	return j
}

// Get returns a job by id if owned by userID.
func (s *CopyJobStore) Get(userID int64, id string) *CopyJob {
	s.mu.Lock()
	j := s.jobs[id]
	s.mu.Unlock()
	if j == nil || j.UserID != userID {
		return nil
	}
	return j
}

// Snapshot returns a copy-safe view for JSON.
func (j *CopyJob) Snapshot() CopyJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	cp := *j
	cp.CopiedNames = append([]string(nil), j.CopiedNames...)
	cp.SkippedPaths = append([]string(nil), j.SkippedPaths...)
	cp.Errors = append([]CopyJobError(nil), j.Errors...)
	return cp
}

func (j *CopyJob) setRunning() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = "running"
	j.UpdatedAt = time.Now()
}

func (j *CopyJob) markCopied(name string, bytes int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Done++
	j.Copied++
	j.BytesDone += bytes
	j.CopiedNames = append(j.CopiedNames, name)
	j.UpdatedAt = time.Now()
}

func (j *CopyJob) markSkipped(rel string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Done++
	j.Skipped++
	j.SkippedPaths = append(j.SkippedPaths, rel)
	j.UpdatedAt = time.Now()
}

func (j *CopyJob) markFailed(rel, msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Done++
	j.Failed++
	j.Errors = append(j.Errors, CopyJobError{RelPath: rel, Error: msg})
	j.UpdatedAt = time.Now()
}

func (j *CopyJob) finish(status, message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = status
	j.Message = message
	j.UpdatedAt = time.Now()
}

func newJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}

// Mark helpers exported for the handler package.
func (j *CopyJob) SetRunning()                         { j.setRunning() }
func (j *CopyJob) MarkCopied(name string, bytes int64)  { j.markCopied(name, bytes) }
func (j *CopyJob) MarkSkipped(rel string)               { j.markSkipped(rel) }
func (j *CopyJob) MarkFailed(rel, msg string)           { j.markFailed(rel, msg) }
func (j *CopyJob) Finish(status, message string)        { j.finish(status, message) }
