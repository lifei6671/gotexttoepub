package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Add(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func TestSubmitLimits(t *testing.T) {
	started := make(chan struct{}, 1)
	unblock := make(chan struct{})
	manager := startTestManager(t, Config{
		DataDir:    filepath.Join(t.TempDir(), "jobs"),
		Workers:    1,
		QueueSize:  1,
		PerIPLimit: 1,
		Convert: func(_ context.Context, _ *Job, inputPath, _ string) (string, int64, error) {
			started <- struct{}{}
			<-unblock
			output := filepath.Join(filepath.Dir(inputPath), outputFile)
			return "book.epub", 4, os.WriteFile(output, []byte("epub"), 0o600)
		},
	})

	first := submitTestJob(t, manager, "192.0.2.1", "owner-1")
	<-started

	tests := []struct {
		name  string
		ip    string
		owner string
		want  error
	}{
		{name: "same IP rejected", ip: "192.0.2.1", owner: "owner-1b", want: ErrIPLimit},
		{name: "different IP enters queue", ip: "192.0.2.2", owner: "owner-2"},
		{name: "global capacity rejected", ip: "192.0.2.3", owner: "owner-3", want: ErrQueueFull},
	}
	var second Job
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := createInput(t, "novel")
			job, err := manager.Submit(context.Background(), SubmitInput{
				InputPath: input,
				ClientIP:  test.ip,
				OwnerHash: test.owner,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("Submit() error = %v, want %v", err, test.want)
			}
			if test.want != nil {
				if _, statErr := os.Stat(input); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("rejected source was not removed: %v", statErr)
				}
			} else {
				second = job
			}
		})
	}

	capacity := manager.Capacity()
	if capacity.Active != 2 || capacity.Queued != 1 || capacity.QueueSize != 1 {
		t.Fatalf("Capacity() = %+v", capacity)
	}
	close(unblock)
	waitForStatus(t, manager, first.ID, first.OwnerHash, StatusSucceeded)
	waitForStatus(t, manager, second.ID, second.OwnerHash, StatusSucceeded)
}

func TestCleanupRetentionHonorsDownloadLease(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)}
	manager := startTestManager(t, Config{
		DataDir:   filepath.Join(t.TempDir(), "jobs"),
		Workers:   1,
		QueueSize: 1,
		Retention: 24 * time.Hour,
		Clock:     clock,
		Convert:   fixedOutput(64),
	})
	job := submitTestJob(t, manager, "192.0.2.10", "owner")
	waitForStatus(t, manager, job.ID, job.OwnerHash, StatusSucceeded)

	_, release, err := manager.AcquireDownload(job.ID, job.OwnerHash)
	if err != nil {
		t.Fatalf("AcquireDownload() error = %v", err)
	}
	clock.Add(25 * time.Hour)
	if err := manager.Cleanup(); err != nil {
		t.Fatalf("Cleanup() with lease error = %v", err)
	}
	current, err := manager.Get(job.ID, job.OwnerHash)
	if err != nil || current.Status != StatusSucceeded {
		t.Fatalf("leased job = %+v, %v", current, err)
	}

	release()
	if err := manager.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	current, err = manager.Get(job.ID, job.OwnerHash)
	if err != nil || current.Status != StatusExpired {
		t.Fatalf("expired job = %+v, %v", current, err)
	}
	if _, _, err := manager.AcquireDownload(job.ID, job.OwnerHash); !errors.Is(err, ErrExpired) {
		t.Fatalf("AcquireDownload() error = %v, want ErrExpired", err)
	}
}

func TestGetExpiresOutputAtDeadlineWithoutWaitingForTicker(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)}
	manager := startTestManager(t, Config{
		DataDir:   filepath.Join(t.TempDir(), "jobs"),
		Workers:   1,
		QueueSize: 1,
		Retention: time.Hour,
		Clock:     clock,
		Convert:   fixedOutput(64),
	})
	job := submitTestJob(t, manager, "192.0.2.11", "owner")
	waitForStatus(t, manager, job.ID, job.OwnerHash, StatusSucceeded)

	clock.Add(time.Hour)
	current, err := manager.Get(job.ID, job.OwnerHash)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current.Status != StatusExpired {
		t.Fatalf("Get() status = %s, want %s", current.Status, StatusExpired)
	}
	if _, _, err := manager.AcquireDownload(job.ID, job.OwnerHash); !errors.Is(err, ErrExpired) {
		t.Fatalf("AcquireDownload() error = %v, want ErrExpired", err)
	}
}

func TestCleanupQuotaEvictsOldestOutput(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)}
	manager := startTestManager(t, Config{
		DataDir:   filepath.Join(t.TempDir(), "jobs"),
		Workers:   1,
		QueueSize: 1,
		Retention: 24 * time.Hour,
		Clock:     clock,
		Convert:   fixedOutput(1024),
	})
	oldest := submitTestJob(t, manager, "192.0.2.20", "oldest")
	waitForStatus(t, manager, oldest.ID, oldest.OwnerHash, StatusSucceeded)
	clock.Add(time.Minute)
	newest := submitTestJob(t, manager, "192.0.2.21", "newest")
	waitForStatus(t, manager, newest.ID, newest.OwnerHash, StatusSucceeded)

	usage, err := directorySize(manager.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.cfg.MaxDiskBytes = usage - 512
	manager.mu.Unlock()
	if err := manager.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	oldestJob, _ := manager.Get(oldest.ID, oldest.OwnerHash)
	newestJob, _ := manager.Get(newest.ID, newest.OwnerHash)
	if oldestJob.Status != StatusEvicted || newestJob.Status != StatusSucceeded {
		t.Fatalf("oldest/newest status = %s/%s", oldestJob.Status, newestJob.Status)
	}
}

func TestRecoveryRequeuesInterruptedJobs(t *testing.T) {
	for _, interruptedStatus := range []Status{StatusQueued, StatusConverting} {
		t.Run(string(interruptedStatus), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "jobs")
			id := strings.Repeat("a", 32)
			dir := filepath.Join(root, id)
			if err := os.MkdirAll(filepath.Join(dir, workDir), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, inputFile), []byte("novel"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, workDir, "partial"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			job := Job{
				ID:           id,
				OwnerHash:    "owner",
				OriginalName: "novel.txt",
				InputSize:    5,
				Status:       interruptedStatus,
				CreatedAt:    time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC),
			}
			raw, err := json.Marshal(job)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, metaFile), raw, 0o600); err != nil {
				t.Fatal(err)
			}

			manager := startTestManager(t, Config{
				DataDir:   root,
				Workers:   1,
				QueueSize: 1,
				Convert:   fixedOutput(32),
			})
			waitForStatus(t, manager, id, "owner", StatusSucceeded)
			if _, err := os.Stat(filepath.Join(dir, workDir)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("work directory was not removed: %v", err)
			}
		})
	}
}

func TestCleanupPurgesTerminalJobAfterRetention(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)}
	manager := startTestManager(t, Config{
		DataDir:   filepath.Join(t.TempDir(), "jobs"),
		Workers:   1,
		QueueSize: 1,
		Retention: time.Hour,
		Clock:     clock,
		Convert: func(context.Context, *Job, string, string) (string, int64, error) {
			return "", 0, errors.New("conversion failed")
		},
	})
	job := submitTestJob(t, manager, "192.0.2.30", "owner")
	waitForStatus(t, manager, job.ID, job.OwnerHash, StatusFailed)
	if _, err := os.Stat(manager.jobDir(job.ID)); err != nil {
		t.Fatalf("terminal job directory missing before retention: %v", err)
	}

	clock.Add(time.Hour)
	if err := manager.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := manager.Get(job.ID, job.OwnerHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(manager.jobDir(job.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("terminal job directory still exists: %v", err)
	}
}

func TestStartRemovesStaleOrphans(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	root := filepath.Join(t.TempDir(), "jobs")
	incoming := filepath.Join(root, "incoming")
	orphan := filepath.Join(root, "corrupt-job")
	if err := os.MkdirAll(incoming, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	staleUpload := filepath.Join(incoming, "upload.tmp")
	if err := os.WriteFile(staleUpload, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-20 * time.Minute)
	for _, path := range []string{staleUpload, orphan} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	manager := startTestManager(t, Config{
		DataDir:   root,
		Workers:   1,
		QueueSize: 1,
		Clock:     clock,
		Convert:   fixedOutput(32),
	})
	for _, path := range []string{staleUpload, orphan} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale orphan %q still exists: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ipKeyFile)); err != nil {
		t.Fatalf("IP HMAC key was removed: %v", err)
	}
	_ = manager
}

func TestRecoveryRestoresPerIPAdmissionLimit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jobs")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("k", ipKeySize))
	if err := os.WriteFile(filepath.Join(root, ipKeyFile), key, 0o600); err != nil {
		t.Fatal(err)
	}
	clientIP := "192.0.2.40"
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(clientIP))
	clientKey := hex.EncodeToString(mac.Sum(nil))

	id := strings.Repeat("b", 32)
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, inputFile), []byte("novel"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := Job{
		ID:           id,
		OwnerHash:    "owner",
		ClientKey:    clientKey,
		Status:       StatusQueued,
		CreatedAt:    time.Now(),
		OriginalName: "novel.txt",
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, metaFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	unblock := make(chan struct{})
	manager := startTestManager(t, Config{
		DataDir:    root,
		Workers:    1,
		QueueSize:  1,
		PerIPLimit: 1,
		Convert: func(_ context.Context, _ *Job, inputPath, _ string) (string, int64, error) {
			<-unblock
			return fixedOutput(32)(context.Background(), nil, inputPath, "")
		},
	})
	if err := manager.CanSubmit(clientIP); !errors.Is(err, ErrIPLimit) {
		t.Fatalf("CanSubmit() error = %v, want ErrIPLimit", err)
	}
	close(unblock)
	waitForStatus(t, manager, id, "owner", StatusSucceeded)
}

func startTestManager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return manager
}

func submitTestJob(t *testing.T, manager *Manager, ip, owner string) Job {
	t.Helper()
	job, err := manager.Submit(context.Background(), SubmitInput{
		InputPath:    createInput(t, "novel"),
		ClientIP:     ip,
		OwnerHash:    owner,
		OriginalName: "novel.txt",
		InputSize:    5,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	return job
}

func createInput(t *testing.T, content string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "input-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

func fixedOutput(size int) ConvertFunc {
	return func(_ context.Context, _ *Job, inputPath, _ string) (string, int64, error) {
		data := []byte(strings.Repeat("e", size))
		err := os.WriteFile(filepath.Join(filepath.Dir(inputPath), outputFile), data, 0o600)
		return "book.epub", int64(size), err
	}
}

func waitForStatus(t *testing.T, manager *Manager, id, owner string, status Status) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.Get(id, owner)
		if err == nil && job.Status == status {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, err := manager.Get(id, owner)
	t.Fatalf("job status = %s (%v), want %s", job.Status, err, status)
	return Job{}
}
