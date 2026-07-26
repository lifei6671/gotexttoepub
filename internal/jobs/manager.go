// Package jobs manages bounded, locally persisted conversion jobs.
package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	inputFile  = "input.txt"
	coverFile  = "cover"
	outputFile = "output.epub"
	metaFile   = "job.json"
	workDir    = "work"
	ipKeyFile  = ".ip-hmac.key"
	ipKeySize  = 32
)

var (
	ErrNotStarted   = errors.New("job manager not started")
	ErrClosed       = errors.New("job manager closed")
	ErrInvalidInput = errors.New("invalid job input")
	ErrQueueFull    = errors.New("conversion queue is full")
	ErrIPLimit      = errors.New("IP concurrent job limit reached")
	ErrStorageFull  = errors.New("job storage limit reached")
	ErrNotFound     = errors.New("job not found")
	ErrNotReady     = errors.New("job output is not ready")
	ErrExpired      = errors.New("job output expired or evicted")
)

type Status string

const (
	StatusQueued     Status = "queued"
	StatusConverting Status = "converting"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusExpired    Status = "expired"
	StatusEvicted    Status = "evicted"
)

// Job is the durable job record. ClientIP is deliberately never serialized.
type Job struct {
	ID            string     `json:"id"`
	OwnerHash     string     `json:"ownerHash"`
	ClientIP      string     `json:"-"`
	ClientKey     string     `json:"clientKey,omitempty"`
	OriginalName  string     `json:"originalName"`
	InputSize     int64      `json:"inputSize"`
	Status        Status     `json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	PurgeAt       *time.Time `json:"purgeAt,omitempty"`
	QueuePosition int        `json:"queuePosition,omitempty"`
	OutputName    string     `json:"outputName,omitempty"`
	OutputSize    int64      `json:"outputSize,omitempty"`
	ErrorCode     string     `json:"errorCode,omitempty"`
}

// SubmitInput transfers ownership of InputPath and CoverPath to Manager.
// Manager removes both source files even when submission is rejected.
type SubmitInput struct {
	InputPath    string
	CoverPath    string
	ClientIP     string
	OwnerHash    string
	OriginalName string
	InputSize    int64
}

type ConvertFunc func(
	context.Context,
	*Job,
	string,
	string,
) (outputName string, outputSize int64, err error)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Config struct {
	DataDir         string
	Workers         int
	QueueSize       int
	PerIPLimit      int
	Retention       time.Duration
	JobTimeout      time.Duration
	CleanupInterval time.Duration
	MaxDiskBytes    int64
	Clock           Clock
	Convert         ConvertFunc
}

type Capacity struct {
	Workers   int
	Active    int
	Queued    int
	QueueSize int
}

type Manager struct {
	cfg Config

	mu              sync.Mutex
	jobs            map[string]*Job
	activeByIP      map[string]int
	downloadLeases  map[string]int
	queueOrder      []string
	recoveryBacklog []string
	queue           chan string
	started         bool
	closed          bool
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	ipHashKey       []byte
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("%w: data directory is required", ErrInvalidInput)
	}
	if cfg.Workers == 0 {
		cfg.Workers = 2
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 8
	}
	if cfg.PerIPLimit == 0 {
		cfg.PerIPLimit = 1
	}
	if cfg.Retention == 0 {
		cfg.Retention = 24 * time.Hour
	}
	if cfg.JobTimeout == 0 {
		cfg.JobTimeout = 10 * time.Minute
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}
	if cfg.Workers < 1 || cfg.QueueSize < 0 || cfg.PerIPLimit < 1 ||
		cfg.Retention < 0 || cfg.JobTimeout < 0 || cfg.CleanupInterval < 0 ||
		cfg.MaxDiskBytes < 0 || cfg.Convert == nil {
		return nil, fmt.Errorf("%w: invalid manager configuration", ErrInvalidInput)
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}

	return &Manager{
		cfg:            cfg,
		jobs:           make(map[string]*Job),
		activeByIP:     make(map[string]int),
		downloadLeases: make(map[string]int),
		queue:          make(chan string, cfg.Workers+cfg.QueueSize),
	}, nil
}

func (m *Manager) Start(parent context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.started {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := os.MkdirAll(m.cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create jobs directory: %w", err)
	}
	if err := os.Chmod(m.cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("protect jobs directory: %w", err)
	}
	key, err := loadOrCreateIPKey(filepath.Join(m.cfg.DataDir, ipKeyFile))
	if err != nil {
		return fmt.Errorf("load IP hash key: %w", err)
	}
	m.ipHashKey = key

	pending, err := m.recoverLocked()
	if err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(parent)
	m.started = true
	for i := 0; i < m.cfg.Workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	m.enqueueRecoveredLocked(pending)
	if m.cfg.CleanupInterval > 0 {
		m.wg.Add(1)
		go m.cleanupLoop()
	}
	return nil
}

func (m *Manager) Submit(_ context.Context, in SubmitInput) (Job, error) {
	cleanupSources := true
	defer func() {
		if cleanupSources {
			removeSources(in.InputPath, in.CoverPath)
		}
	}()

	if in.InputPath == "" || in.ClientIP == "" || in.OwnerHash == "" {
		return Job{}, fmt.Errorf("%w: input path, client IP and owner hash are required", ErrInvalidInput)
	}
	inputInfo, err := os.Stat(in.InputPath)
	if err != nil || !inputInfo.Mode().IsRegular() {
		return Job{}, fmt.Errorf("%w: input file is unavailable", ErrInvalidInput)
	}
	if in.InputSize <= 0 {
		in.InputSize = inputInfo.Size()
	}
	incomingBytes := inputInfo.Size()
	if in.CoverPath != "" {
		coverInfo, statErr := os.Stat(in.CoverPath)
		if statErr != nil || !coverInfo.Mode().IsRegular() {
			return Job{}, fmt.Errorf("%w: cover file is unavailable", ErrInvalidInput)
		}
		incomingBytes += coverInfo.Size()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return Job{}, ErrNotStarted
	}
	if m.closed {
		return Job{}, ErrClosed
	}
	clientKey := m.clientKey(in.ClientIP)
	if m.activeByIP[clientKey] >= m.cfg.PerIPLimit {
		return Job{}, ErrIPLimit
	}
	if m.activeCountLocked() >= m.cfg.Workers+m.cfg.QueueSize {
		return Job{}, ErrQueueFull
	}
	if err := m.ensureStorageLocked(incomingBytes); err != nil {
		return Job{}, err
	}

	id, err := newID()
	if err != nil {
		return Job{}, fmt.Errorf("generate job id: %w", err)
	}
	dir := m.jobDir(id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Job{}, fmt.Errorf("create job directory: %w", err)
	}
	if err := moveOwnedFile(in.InputPath, filepath.Join(dir, inputFile)); err != nil {
		_ = os.RemoveAll(dir)
		return Job{}, fmt.Errorf("store input file: %w", err)
	}
	in.InputPath = ""
	if in.CoverPath != "" {
		if err := moveOwnedFile(in.CoverPath, filepath.Join(dir, coverFile)); err != nil {
			_ = os.RemoveAll(dir)
			return Job{}, fmt.Errorf("store cover file: %w", err)
		}
		in.CoverPath = ""
	}

	now := m.cfg.Clock.Now()
	job := &Job{
		ID:            id,
		OwnerHash:     in.OwnerHash,
		ClientIP:      in.ClientIP,
		ClientKey:     clientKey,
		OriginalName:  in.OriginalName,
		InputSize:     in.InputSize,
		Status:        StatusQueued,
		CreatedAt:     now,
		QueuePosition: len(m.queueOrder) + 1,
	}
	if err := m.persistLocked(job); err != nil {
		_ = os.RemoveAll(dir)
		return Job{}, fmt.Errorf("persist submitted job: %w", err)
	}
	m.jobs[id] = job
	m.activeByIP[clientKey]++
	m.queueOrder = append(m.queueOrder, id)
	m.recalculateQueuePositionsLocked()
	m.queue <- id
	cleanupSources = false
	return *job, nil
}

func (m *Manager) Get(id, ownerHash string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[id]
	if job == nil || job.OwnerHash != ownerHash {
		return Job{}, ErrNotFound
	}
	if m.isExpiredLocked(job) {
		if err := m.removeOutputLocked(job, StatusExpired); err != nil {
			return Job{}, err
		}
	}
	return *job, nil
}

// CanSubmit performs a read-only admission check before an HTTP handler receives
// and validates an upload. Submit always repeats the check before taking ownership.
func (m *Manager) CanSubmit(clientIP string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return ErrNotStarted
	}
	if m.closed {
		return ErrClosed
	}
	if clientIP == "" {
		return ErrInvalidInput
	}
	if m.activeByIP[m.clientKey(clientIP)] >= m.cfg.PerIPLimit {
		return ErrIPLimit
	}
	if m.activeCountLocked() >= m.cfg.Workers+m.cfg.QueueSize {
		return ErrQueueFull
	}
	return nil
}

func (m *Manager) AcquireDownload(id, ownerHash string) (string, func(), error) {
	m.mu.Lock()
	job := m.jobs[id]
	if job == nil || job.OwnerHash != ownerHash {
		m.mu.Unlock()
		return "", nil, ErrNotFound
	}
	if job.Status == StatusExpired || job.Status == StatusEvicted {
		m.mu.Unlock()
		return "", nil, ErrExpired
	}
	if m.isExpiredLocked(job) {
		if err := m.removeOutputLocked(job, StatusExpired); err != nil {
			m.mu.Unlock()
			return "", nil, err
		}
		m.mu.Unlock()
		return "", nil, ErrExpired
	}
	if job.Status != StatusSucceeded {
		m.mu.Unlock()
		return "", nil, ErrNotReady
	}
	path := filepath.Join(m.jobDir(id), outputFile)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		m.mu.Unlock()
		return "", nil, ErrExpired
	}
	m.downloadLeases[id]++
	m.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.downloadLeases[id]--
			if m.downloadLeases[id] <= 0 {
				delete(m.downloadLeases, id)
			}
		})
	}
	return path, release, nil
}

func (m *Manager) Capacity() Capacity {
	m.mu.Lock()
	defer m.mu.Unlock()
	active := 0
	queued := 0
	for _, job := range m.jobs {
		switch job.Status {
		case StatusQueued:
			active++
			queued++
		case StatusConverting:
			active++
		}
	}
	return Capacity{
		Workers:   m.cfg.Workers,
		Active:    active,
		Queued:    queued,
		QueueSize: m.cfg.QueueSize,
	}
}

// Cleanup applies retention first, then evicts oldest completed outputs until
// the configured disk limit is met. Active downloads are never removed.
func (m *Manager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return ErrNotStarted
	}
	if m.closed {
		return ErrClosed
	}
	return m.cleanupLocked(0)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case id := <-m.queue:
			m.runJob(id)
		}
	}
}

func (m *Manager) runJob(id string) {
	m.mu.Lock()
	job := m.jobs[id]
	if job == nil || job.Status != StatusQueued {
		m.mu.Unlock()
		return
	}
	m.removeFromQueueLocked(id)
	job.Status = StatusConverting
	job.QueuePosition = 0
	_ = m.persistLocked(job)
	snapshot := *job
	inputPath := filepath.Join(m.jobDir(id), inputFile)
	coverPath := filepath.Join(m.jobDir(id), coverFile)
	if _, err := os.Stat(coverPath); errors.Is(err, fs.ErrNotExist) {
		coverPath = ""
	}
	m.mu.Unlock()

	ctx := m.ctx
	cancel := func() {}
	if m.cfg.JobTimeout > 0 {
		ctx, cancel = context.WithTimeout(m.ctx, m.cfg.JobTimeout)
	}
	outputName, _, convertErr := m.cfg.Convert(ctx, &snapshot, inputPath, coverPath)
	cancel()

	m.mu.Lock()
	defer m.mu.Unlock()
	job = m.jobs[id]
	if job == nil {
		return
	}
	now := m.cfg.Clock.Now()
	if convertErr == nil {
		info, statErr := os.Stat(filepath.Join(m.jobDir(id), outputFile))
		if statErr != nil || !info.Mode().IsRegular() {
			convertErr = errors.New("converter did not create output.epub")
		} else {
			job.Status = StatusSucceeded
			job.OutputName = outputName
			if job.OutputName == "" {
				job.OutputName = outputFile
			}
			job.OutputSize = info.Size()
			job.CompletedAt = timePtr(now)
			expires := now.Add(m.cfg.Retention)
			job.ExpiresAt = &expires
			job.PurgeAt = nil
			job.ErrorCode = ""
		}
	}
	if convertErr != nil {
		job.Status = StatusFailed
		job.OutputName = ""
		job.OutputSize = 0
		job.CompletedAt = timePtr(now)
		job.ExpiresAt = nil
		purgeAt := now.Add(m.cfg.Retention)
		job.PurgeAt = &purgeAt
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			job.ErrorCode = "timeout"
		} else {
			job.ErrorCode = "conversion_failed"
		}
		_ = os.Remove(filepath.Join(m.jobDir(id), outputFile))
	}
	m.finishIPLocked(job)
	_ = os.Remove(filepath.Join(m.jobDir(id), inputFile))
	_ = os.Remove(filepath.Join(m.jobDir(id), coverFile))
	_ = os.RemoveAll(filepath.Join(m.jobDir(id), workDir))
	_ = m.persistLocked(job)
	_ = m.cleanupLocked(0)
	m.promoteRecoveryLocked()
}

func (m *Manager) cleanupLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.cfg.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			_ = m.cleanupLocked(0)
			m.mu.Unlock()
		}
	}
}

func (m *Manager) cleanupLocked(requiredFree int64) error {
	now := m.cfg.Clock.Now()
	if err := m.cleanupOrphansLocked(now); err != nil {
		return err
	}
	for _, job := range m.jobs {
		if job.Status == StatusSucceeded && job.ExpiresAt != nil &&
			!job.ExpiresAt.After(now) && m.downloadLeases[job.ID] == 0 {
			if err := m.removeOutputLocked(job, StatusExpired); err != nil {
				return err
			}
		}
	}
	for id, job := range m.jobs {
		if job.PurgeAt != nil && !job.PurgeAt.After(now) &&
			m.downloadLeases[job.ID] == 0 && isTerminal(job.Status) {
			if err := os.RemoveAll(m.jobDir(job.ID)); err != nil {
				return fmt.Errorf("remove terminal job: %w", err)
			}
			delete(m.jobs, id)
		}
	}
	if m.cfg.MaxDiskBytes <= 0 {
		return nil
	}

	usage, err := directorySize(m.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("measure job storage: %w", err)
	}
	target := m.cfg.MaxDiskBytes - requiredFree
	if target < 0 {
		return ErrStorageFull
	}
	if usage <= target {
		return nil
	}
	candidates := make([]*Job, 0)
	for _, job := range m.jobs {
		if job.Status == StatusSucceeded && job.CompletedAt != nil &&
			m.downloadLeases[job.ID] == 0 {
			candidates = append(candidates, job)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CompletedAt.Before(*candidates[j].CompletedAt)
	})
	for _, job := range candidates {
		if err := m.removeOutputLocked(job, StatusEvicted); err != nil {
			return err
		}
		usage, err = directorySize(m.cfg.DataDir)
		if err != nil {
			return fmt.Errorf("measure job storage: %w", err)
		}
		if usage <= target {
			return nil
		}
	}
	return ErrStorageFull
}

func (m *Manager) ensureStorageLocked(incomingBytes int64) error {
	if m.cfg.MaxDiskBytes <= 0 {
		return nil
	}
	if err := m.cleanupLocked(incomingBytes); err != nil {
		if errors.Is(err, ErrStorageFull) {
			return ErrStorageFull
		}
		return err
	}
	return nil
}

// cleanupOrphansLocked removes stale temporary uploads and directories that do
// not have a recoverable job record. The grace period keeps in-flight HTTP
// uploads away from the cleanup loop.
func (m *Manager) cleanupOrphansLocked(now time.Time) error {
	grace := m.cfg.JobTimeout
	if grace < 10*time.Minute {
		grace = 10 * time.Minute
	}
	staleBefore := now.Add(-grace)
	entries, err := os.ReadDir(m.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("scan data directory for orphan files: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(m.cfg.DataDir, name)
		if name == ipKeyFile {
			continue
		}
		if name == "incoming" && entry.IsDir() {
			children, readErr := os.ReadDir(path)
			if readErr != nil {
				return fmt.Errorf("scan incoming directory: %w", readErr)
			}
			for _, child := range children {
				childPath := filepath.Join(path, child.Name())
				info, infoErr := child.Info()
				if infoErr != nil {
					return fmt.Errorf("inspect incoming file: %w", infoErr)
				}
				if info.ModTime().After(staleBefore) {
					continue
				}
				if removeErr := os.RemoveAll(childPath); removeErr != nil {
					return fmt.Errorf("remove stale incoming file: %w", removeErr)
				}
			}
			continue
		}
		if entry.IsDir() {
			if _, exists := m.jobs[name]; exists {
				continue
			}
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("inspect orphan path: %w", infoErr)
		}
		if info.ModTime().After(staleBefore) {
			continue
		}
		if removeErr := os.RemoveAll(path); removeErr != nil {
			return fmt.Errorf("remove orphan path: %w", removeErr)
		}
	}
	return nil
}

func (m *Manager) removeOutputLocked(job *Job, status Status) error {
	if err := os.Remove(filepath.Join(m.jobDir(job.ID), outputFile)); err != nil &&
		!errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove job output: %w", err)
	}
	job.Status = status
	job.OutputSize = 0
	job.ErrorCode = string(status)
	purgeAt := m.cfg.Clock.Now().Add(m.cfg.Retention)
	job.PurgeAt = &purgeAt
	return m.persistLocked(job)
}

func (m *Manager) isExpiredLocked(job *Job) bool {
	return job.Status == StatusSucceeded &&
		job.ExpiresAt != nil &&
		!job.ExpiresAt.After(m.cfg.Clock.Now()) &&
		m.downloadLeases[job.ID] == 0
}

func (m *Manager) recoverLocked() ([]string, error) {
	entries, err := os.ReadDir(m.cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("scan jobs directory: %w", err)
	}
	var pending []*Job
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(m.cfg.DataDir, entry.Name())
		raw, readErr := os.ReadFile(filepath.Join(dir, metaFile))
		if readErr != nil {
			continue
		}
		var job Job
		if json.Unmarshal(raw, &job) != nil || job.ID == "" || job.ID != entry.Name() {
			continue
		}
		m.jobs[job.ID] = &job
		_ = os.Remove(filepath.Join(dir, metaFile+".tmp"))
		_ = os.RemoveAll(filepath.Join(dir, workDir))
		switch job.Status {
		case StatusQueued, StatusConverting:
			if info, statErr := os.Stat(filepath.Join(dir, inputFile)); statErr == nil &&
				info.Mode().IsRegular() {
				job.Status = StatusQueued
				job.QueuePosition = 0
				if job.ClientKey != "" {
					m.activeByIP[job.ClientKey]++
				}
				pending = append(pending, &job)
			} else {
				now := m.cfg.Clock.Now()
				job.Status = StatusFailed
				job.CompletedAt = &now
				job.ErrorCode = "input_missing"
				purgeAt := now.Add(m.cfg.Retention)
				job.PurgeAt = &purgeAt
				_ = m.persistLocked(&job)
			}
		case StatusSucceeded:
			if info, statErr := os.Stat(filepath.Join(dir, outputFile)); statErr != nil ||
				!info.Mode().IsRegular() {
				job.Status = StatusExpired
				job.OutputSize = 0
				job.ErrorCode = "output_missing"
				purgeAt := m.cfg.Clock.Now().Add(m.cfg.Retention)
				job.PurgeAt = &purgeAt
				_ = m.persistLocked(&job)
			}
		case StatusFailed, StatusExpired, StatusEvicted:
			if job.PurgeAt == nil {
				base := m.cfg.Clock.Now()
				if job.CompletedAt != nil {
					base = *job.CompletedAt
				}
				purgeAt := base.Add(m.cfg.Retention)
				job.PurgeAt = &purgeAt
				_ = m.persistLocked(&job)
			}
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})
	ids := make([]string, 0, len(pending))
	for _, job := range pending {
		ids = append(ids, job.ID)
		m.queueOrder = append(m.queueOrder, job.ID)
	}
	m.recalculateQueuePositionsLocked()
	if err := m.cleanupLocked(0); err != nil && !errors.Is(err, ErrStorageFull) {
		return nil, err
	}
	return ids, nil
}

func (m *Manager) enqueueRecoveredLocked(ids []string) {
	limit := cap(m.queue)
	if len(ids) < limit {
		limit = len(ids)
	}
	for _, id := range ids[:limit] {
		m.queue <- id
	}
	m.recoveryBacklog = append(m.recoveryBacklog, ids[limit:]...)
}

func (m *Manager) promoteRecoveryLocked() {
	if len(m.recoveryBacklog) == 0 {
		return
	}
	id := m.recoveryBacklog[0]
	m.recoveryBacklog = m.recoveryBacklog[1:]
	m.queue <- id
}

func (m *Manager) persistLocked(job *Job) error {
	dir := m.jobDir(job.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.OpenFile(
		filepath.Join(dir, metaFile+".tmp"),
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o600,
	)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(job); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := replaceFile(tmp.Name(), filepath.Join(dir, metaFile)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func (m *Manager) recalculateQueuePositionsLocked() {
	for index, id := range m.queueOrder {
		if job := m.jobs[id]; job != nil && job.Status == StatusQueued {
			job.QueuePosition = index + 1
		}
	}
}

func (m *Manager) removeFromQueueLocked(id string) {
	for i, queuedID := range m.queueOrder {
		if queuedID == id {
			m.queueOrder = append(m.queueOrder[:i], m.queueOrder[i+1:]...)
			break
		}
	}
	m.recalculateQueuePositionsLocked()
}

func (m *Manager) activeCountLocked() int {
	count := 0
	for _, job := range m.jobs {
		if job.Status == StatusQueued || job.Status == StatusConverting {
			count++
		}
	}
	return count
}

func (m *Manager) finishIPLocked(job *Job) {
	if job.ClientKey == "" {
		return
	}
	m.activeByIP[job.ClientKey]--
	if m.activeByIP[job.ClientKey] <= 0 {
		delete(m.activeByIP, job.ClientKey)
	}
}

func (m *Manager) jobDir(id string) string {
	return filepath.Join(m.cfg.DataDir, id)
}

func moveOwnedFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return os.Chmod(destination, 0o600)
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	if syncErr := out.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	return os.Remove(source)
}

func removeSources(paths ...string) {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		_ = os.Remove(path)
	}
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func newID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func timePtr(value time.Time) *time.Time { return &value }

func (m *Manager) clientKey(clientIP string) string {
	mac := hmac.New(sha256.New, m.ipHashKey)
	_, _ = mac.Write([]byte(clientIP))
	return hex.EncodeToString(mac.Sum(nil))
}

func loadOrCreateIPKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != ipKeySize {
			return nil, errors.New("IP hash key has invalid length")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, err
		}
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, ipKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err = file.Write(key); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return key, nil
}

func isTerminal(status Status) bool {
	return status == StatusFailed || status == StatusExpired || status == StatusEvicted
}
