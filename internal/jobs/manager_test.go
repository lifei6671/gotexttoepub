package jobs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func TestRunJobMarksAutomaticCoverFailure(t *testing.T) {
	manager := startTestManager(t, Config{
		DataDir:   filepath.Join(t.TempDir(), "jobs"),
		Workers:   1,
		QueueSize: 1,
		Convert: func(context.Context, *Job, string, string) (string, int64, error) {
			return "", 0, ErrCoverGeneration
		},
	})

	job := submitTestJob(t, manager, "192.0.2.6", "owner")
	failed := waitForStatus(t, manager, job.ID, job.OwnerHash, StatusFailed)
	if failed.ErrorCode != "cover_generation_failed" {
		t.Fatalf("ErrorCode = %q, want cover_generation_failed", failed.ErrorCode)
	}
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
	var logs bytes.Buffer
	manager := startTestManager(t, Config{
		DataDir:   filepath.Join(t.TempDir(), "jobs"),
		Workers:   1,
		QueueSize: 1,
		Retention: time.Hour,
		Clock:     clock,
		Logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
		Convert: func(context.Context, *Job, string, string) (string, int64, error) {
			return "", 0, errors.New("conversion failed")
		},
	})
	job := submitTestJob(t, manager, "192.0.2.30", "owner")
	waitForStatus(t, manager, job.ID, job.OwnerHash, StatusFailed)
	if !strings.Contains(logs.String(), `"msg":"转换任务失败"`) || !strings.Contains(logs.String(), `"error_code":"conversion_failed"`) {
		t.Fatalf("conversion failure was not logged: %s", logs.String())
	}
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

func TestReviewRequiredPreservesSourcesAndReport(t *testing.T) {
	report := testInspectionReport()
	manager := startTestManager(t, Config{
		DataDir:    filepath.Join(t.TempDir(), "jobs"),
		Workers:    1,
		QueueSize:  1,
		PerIPLimit: 1,
		Retention:  time.Hour,
		Convert: func(context.Context, *Job, string, string) (string, int64, error) {
			return "", 0, &ReviewRequiredError{Report: report}
		},
	})
	input := createInput(t, "novel")
	cover := createInput(t, "cover")
	job, err := manager.Submit(context.Background(), SubmitInput{
		InputPath: input,
		CoverPath: cover,
		ClientIP:  "192.0.2.50",
		OwnerHash: "owner",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	current := waitForStatus(t, manager, job.ID, job.OwnerHash, StatusReviewRequired)
	if current.Inspection == nil || current.Inspection.Version != report.Version ||
		len(current.Inspection.Issues) != len(report.Issues) {
		t.Fatalf("inspection = %+v, want %+v", current.Inspection, report)
	}
	if current.ReviewExpiresAt == nil || current.AllowAnomalies {
		t.Fatalf("review state = %+v", current)
	}
	for _, name := range []string{inputFile, coverFile} {
		if _, err := os.Stat(filepath.Join(manager.jobDir(job.ID), name)); err != nil {
			t.Fatalf("retained source %q missing: %v", name, err)
		}
	}
	if err := manager.CanSubmit("192.0.2.50"); err != nil {
		t.Fatalf("CanSubmit() error = %v, want nil", err)
	}
	capacity := manager.Capacity()
	if capacity.Active != 0 || capacity.Queued != 0 {
		t.Fatalf("Capacity() = %+v", capacity)
	}

	raw, err := os.ReadFile(filepath.Join(manager.jobDir(job.ID), metaFile))
	if err != nil {
		t.Fatal(err)
	}
	var durable Job
	if err := json.Unmarshal(raw, &durable); err != nil {
		t.Fatal(err)
	}
	if durable.Status != StatusReviewRequired || durable.Inspection == nil ||
		durable.ReviewExpiresAt == nil {
		t.Fatalf("persisted review = %+v", durable)
	}
}

func TestContinueReviewRechecksPerIPAdmission(t *testing.T) {
	started := make(chan struct{}, 1)
	unblock := make(chan struct{})
	manager := startTestManager(t, Config{
		DataDir:    filepath.Join(t.TempDir(), "jobs"),
		Workers:    1,
		QueueSize:  1,
		PerIPLimit: 1,
		Convert: func(_ context.Context, job *Job, inputPath, _ string) (string, int64, error) {
			content, err := os.ReadFile(inputPath)
			if err != nil {
				return "", 0, err
			}
			if string(content) == "review" && !job.AllowAnomalies {
				return "", 0, ReviewRequiredError{Report: testInspectionReport()}
			}
			started <- struct{}{}
			<-unblock
			return fixedOutput(32)(context.Background(), nil, inputPath, "")
		},
	})
	reviewInput := createInput(t, "review")
	review, err := manager.Submit(context.Background(), SubmitInput{
		InputPath: reviewInput,
		ClientIP:  "192.0.2.55",
		OwnerHash: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, manager, review.ID, review.OwnerHash, StatusReviewRequired)

	activeInput := createInput(t, "active")
	active, err := manager.Submit(context.Background(), SubmitInput{
		InputPath: activeInput,
		ClientIP:  "192.0.2.55",
		OwnerHash: "owner",
	})
	if err != nil {
		t.Fatalf("Submit() after review wait error = %v", err)
	}
	<-started
	if _, err := manager.ContinueReview(review.ID, review.OwnerHash); !errors.Is(err, ErrIPLimit) {
		t.Fatalf("ContinueReview() error = %v, want ErrIPLimit", err)
	}
	close(unblock)
	waitForStatus(t, manager, active.ID, active.OwnerHash, StatusSucceeded)
	if _, err := manager.ContinueReview(review.ID, review.OwnerHash); err != nil {
		t.Fatalf("ContinueReview() after active job completes error = %v", err)
	}
	waitForStatus(t, manager, review.ID, review.OwnerHash, StatusSucceeded)
}

func TestContinueReviewRechecksQueueCapacity(t *testing.T) {
	started := make(chan struct{}, 2)
	unblock := make(chan struct{})
	manager := startTestManager(t, Config{
		DataDir:    filepath.Join(t.TempDir(), "jobs"),
		Workers:    1,
		QueueSize:  1,
		PerIPLimit: 2,
		Convert: func(_ context.Context, job *Job, inputPath, _ string) (string, int64, error) {
			content, err := os.ReadFile(inputPath)
			if err != nil {
				return "", 0, err
			}
			if string(content) == "review" && !job.AllowAnomalies {
				return "", 0, ReviewRequiredError{Report: testInspectionReport()}
			}
			started <- struct{}{}
			<-unblock
			return fixedOutput(32)(context.Background(), nil, inputPath, "")
		},
	})
	review := submitNamedTestJob(t, manager, "review", "192.0.2.56", "owner")
	waitForStatus(t, manager, review.ID, review.OwnerHash, StatusReviewRequired)
	first := submitNamedTestJob(t, manager, "first", "192.0.2.57", "owner-a")
	<-started
	second := submitNamedTestJob(t, manager, "second", "192.0.2.58", "owner-b")
	if _, err := manager.ContinueReview(review.ID, review.OwnerHash); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("ContinueReview() error = %v, want ErrQueueFull", err)
	}
	close(unblock)
	waitForStatus(t, manager, first.ID, first.OwnerHash, StatusSucceeded)
	waitForStatus(t, manager, second.ID, second.OwnerHash, StatusSucceeded)
	if _, err := manager.ContinueReview(review.ID, review.OwnerHash); err != nil {
		t.Fatalf("ContinueReview() after queue drains error = %v", err)
	}
	waitForStatus(t, manager, review.ID, review.OwnerHash, StatusSucceeded)
}

func TestConfirmedReviewDoesNotBlockFollowingQueuedJob(t *testing.T) {
	started := make(chan string, 2)
	allowReview := make(chan struct{})
	allowFollowing := make(chan struct{})
	manager := startTestManager(t, Config{
		DataDir:    filepath.Join(t.TempDir(), "jobs"),
		Workers:    1,
		QueueSize:  1,
		PerIPLimit: 2,
		Convert: func(_ context.Context, job *Job, inputPath, _ string) (string, int64, error) {
			content, err := os.ReadFile(inputPath)
			if err != nil {
				return "", 0, err
			}
			switch string(content) {
			case "review":
				if !job.AllowAnomalies {
					return "", 0, ReviewRequiredError{Report: testInspectionReport()}
				}
				started <- "review"
				<-allowReview
			case "following":
				started <- "following"
				<-allowFollowing
			}
			return fixedOutput(32)(context.Background(), job, inputPath, "")
		},
	})

	review := submitNamedTestJob(t, manager, "review", "192.0.2.70", "review-owner")
	waitForStatus(t, manager, review.ID, review.OwnerHash, StatusReviewRequired)

	if _, err := manager.ContinueReview(review.ID, review.OwnerHash); err != nil {
		t.Fatalf("ContinueReview() error = %v", err)
	}
	select {
	case got := <-started:
		if got != "review" {
			t.Fatalf("started job = %q, want review", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("confirmed review job did not restart")
	}

	following := submitNamedTestJob(t, manager, "following", "192.0.2.71", "following-owner")
	close(allowReview)
	waitForStatus(t, manager, review.ID, review.OwnerHash, StatusSucceeded)
	select {
	case got := <-started:
		if got != "following" {
			t.Fatalf("started job = %q, want following", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("following queued job did not start after confirmed review completed")
	}
	close(allowFollowing)
	waitForStatus(t, manager, following.ID, following.OwnerHash, StatusSucceeded)
}

func TestContinueReviewIsIdempotentUnderConcurrency(t *testing.T) {
	var calls atomic.Int32
	manager := startTestManager(t, Config{
		DataDir:   filepath.Join(t.TempDir(), "jobs"),
		Workers:   1,
		QueueSize: 1,
		Convert: func(_ context.Context, job *Job, inputPath, _ string) (string, int64, error) {
			calls.Add(1)
			if !job.AllowAnomalies {
				return "", 0, ReviewRequiredError{Report: testInspectionReport()}
			}
			return fixedOutput(32)(context.Background(), job, inputPath, "")
		},
	})
	job := submitTestJob(t, manager, "192.0.2.51", "owner")
	waitForStatus(t, manager, job.ID, job.OwnerHash, StatusReviewRequired)

	const callers = 24
	errorsFound := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			continued, err := manager.ContinueReview(job.ID, job.OwnerHash)
			if err == nil && !continued.AllowAnomalies {
				err = errors.New("continued job did not allow anomalies")
			}
			errorsFound <- err
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Errorf("ContinueReview() error = %v", err)
		}
	}
	waitForStatus(t, manager, job.ID, job.OwnerHash, StatusSucceeded)
	if got := calls.Load(); got != 2 {
		t.Fatalf("converter calls = %d, want 2", got)
	}
}

func TestReviewRequiredRecoveryWaitsForConfirmation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jobs")
	first, err := NewManager(Config{
		DataDir:   root,
		Workers:   1,
		QueueSize: 1,
		Convert: func(context.Context, *Job, string, string) (string, int64, error) {
			return "", 0, &ReviewRequiredError{Report: testInspectionReport()}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	clientIP := "192.0.2.52"
	job := submitTestJob(t, first, clientIP, "owner")
	waitForStatus(t, first, job.ID, job.OwnerHash, StatusReviewRequired)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	second := startTestManager(t, Config{
		DataDir:   root,
		Workers:   1,
		QueueSize: 1,
		Convert: func(ctx context.Context, job *Job, inputPath, coverPath string) (string, int64, error) {
			calls.Add(1)
			return fixedOutput(32)(ctx, job, inputPath, coverPath)
		},
	})
	time.Sleep(25 * time.Millisecond)
	recovered, err := second.Get(job.ID, job.OwnerHash)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusReviewRequired || recovered.Inspection == nil || calls.Load() != 0 {
		t.Fatalf("recovered job = %+v, converter calls = %d", recovered, calls.Load())
	}
	if err := second.CanSubmit(clientIP); err != nil {
		t.Fatalf("CanSubmit() recovered review error = %v, want nil", err)
	}
	if _, err := second.ContinueReview(job.ID, job.OwnerHash); err != nil {
		t.Fatalf("ContinueReview() error = %v", err)
	}
	waitForStatus(t, second, job.ID, job.OwnerHash, StatusSucceeded)
	if err := second.CanSubmit(clientIP); err != nil {
		t.Fatalf("CanSubmit() completed review error = %v", err)
	}
}

func TestDiscardReviewDeletesJobAndReleasesIP(t *testing.T) {
	manager := startTestManager(t, Config{
		DataDir:    filepath.Join(t.TempDir(), "jobs"),
		Workers:    1,
		QueueSize:  1,
		PerIPLimit: 1,
		Convert: func(context.Context, *Job, string, string) (string, int64, error) {
			return "", 0, &ReviewRequiredError{Report: testInspectionReport()}
		},
	})
	job := submitTestJob(t, manager, "192.0.2.53", "owner")
	waitForStatus(t, manager, job.ID, job.OwnerHash, StatusReviewRequired)
	if err := manager.DiscardReview(job.ID, "wrong-owner"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DiscardReview() wrong owner error = %v, want ErrNotFound", err)
	}
	if err := manager.DiscardReview(job.ID, job.OwnerHash); err != nil {
		t.Fatalf("DiscardReview() error = %v", err)
	}
	if _, err := manager.Get(job.ID, job.OwnerHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(manager.jobDir(job.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discarded job directory still exists: %v", err)
	}
	if err := manager.CanSubmit("192.0.2.53"); err != nil {
		t.Fatalf("CanSubmit() after discard error = %v", err)
	}
}

func TestCleanupRemovesExpiredReview(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)}
	manager := startTestManager(t, Config{
		DataDir:         filepath.Join(t.TempDir(), "jobs"),
		Workers:         1,
		QueueSize:       1,
		PerIPLimit:      1,
		Retention:       24 * time.Hour,
		ReviewRetention: 10 * time.Minute,
		Clock:           clock,
		Convert: func(context.Context, *Job, string, string) (string, int64, error) {
			return "", 0, &ReviewRequiredError{Report: testInspectionReport()}
		},
	})
	job := submitTestJob(t, manager, "192.0.2.54", "owner")
	waitForStatus(t, manager, job.ID, job.OwnerHash, StatusReviewRequired)
	clock.Add(10 * time.Minute)
	if err := manager.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := manager.Get(job.ID, job.OwnerHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
	if err := manager.CanSubmit("192.0.2.54"); err != nil {
		t.Fatalf("CanSubmit() after cleanup error = %v", err)
	}
}

func TestReviewAdmissionLimits(t *testing.T) {
	tests := []struct {
		name          string
		firstContent  string
		secondContent string
		firstIP       string
		secondIP      string
		config        Config
		wantErrorCode string
	}{
		{
			name:          "per IP limit",
			firstContent:  "first",
			secondContent: "second",
			firstIP:       "192.0.2.60",
			secondIP:      "192.0.2.60",
			config: Config{
				Workers:                1,
				QueueSize:              1,
				MaxPendingReviewsPerIP: 1,
				MaxPendingReviews:      3,
				MaxPendingReviewBytes:  1 << 20,
			},
			wantErrorCode: "review_ip_limit",
		},
		{
			name:          "global count limit",
			firstContent:  "first",
			secondContent: "second",
			firstIP:       "192.0.2.61",
			secondIP:      "192.0.2.62",
			config: Config{
				Workers:                1,
				QueueSize:              1,
				MaxPendingReviewsPerIP: 2,
				MaxPendingReviews:      1,
				MaxPendingReviewBytes:  1 << 20,
			},
			wantErrorCode: "review_limit_reached",
		},
		{
			name:          "global bytes limit",
			firstContent:  "first",
			secondContent: strings.Repeat("x", 20_000),
			firstIP:       "192.0.2.63",
			secondIP:      "192.0.2.64",
			config: Config{
				Workers:                1,
				QueueSize:              1,
				MaxPendingReviewsPerIP: 2,
				MaxPendingReviews:      3,
				MaxPendingReviewBytes:  10_000,
			},
			wantErrorCode: "review_storage_limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.DataDir = filepath.Join(t.TempDir(), "jobs")
			tt.config.Convert = func(context.Context, *Job, string, string) (string, int64, error) {
				return "", 0, ReviewRequiredError{Report: testInspectionReport()}
			}
			manager := startTestManager(t, tt.config)
			first := submitNamedTestJob(t, manager, tt.firstContent, tt.firstIP, "owner-a")
			waitForStatus(t, manager, first.ID, first.OwnerHash, StatusReviewRequired)
			second := submitNamedTestJob(t, manager, tt.secondContent, tt.secondIP, "owner-b")
			failed := waitForStatus(t, manager, second.ID, second.OwnerHash, StatusFailed)
			if failed.ErrorCode != tt.wantErrorCode {
				t.Fatalf("ErrorCode = %q, want %q", failed.ErrorCode, tt.wantErrorCode)
			}
			if _, err := os.Stat(filepath.Join(manager.jobDir(second.ID), inputFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected review source stat error = %v, want not exist", err)
			}
		})
	}
}

func TestCleanupEvictsOldestReviewUnderStoragePressure(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)}
	manager := startTestManager(t, Config{
		DataDir:      filepath.Join(t.TempDir(), "jobs"),
		Workers:      1,
		QueueSize:    1,
		Clock:        clock,
		MaxDiskBytes: 1 << 20,
		Convert: func(context.Context, *Job, string, string) (string, int64, error) {
			return "", 0, ReviewRequiredError{Report: testInspectionReport()}
		},
	})
	first := submitNamedTestJob(t, manager, strings.Repeat("a", 4096), "192.0.2.65", "owner-a")
	waitForStatus(t, manager, first.ID, first.OwnerHash, StatusReviewRequired)
	clock.Add(time.Minute)
	second := submitNamedTestJob(t, manager, strings.Repeat("b", 4096), "192.0.2.66", "owner-b")
	waitForStatus(t, manager, second.ID, second.OwnerHash, StatusReviewRequired)

	usage, err := directorySize(manager.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.cfg.MaxDiskBytes = usage - 1
	manager.mu.Unlock()
	if err := manager.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	evicted, err := manager.Get(first.ID, first.OwnerHash)
	if err != nil {
		t.Fatal(err)
	}
	if evicted.Status != StatusEvicted || evicted.ErrorCode != "review_storage_evicted" {
		t.Fatalf("oldest review = %+v", evicted)
	}
	if _, err := os.Stat(filepath.Join(manager.jobDir(first.ID), inputFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("evicted review source stat error = %v, want not exist", err)
	}
	remaining, err := manager.Get(second.ID, second.OwnerHash)
	if err != nil || remaining.Status != StatusReviewRequired {
		t.Fatalf("newer review = %+v, %v", remaining, err)
	}
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
	return submitNamedTestJob(t, manager, "novel", ip, owner)
}

func submitNamedTestJob(t *testing.T, manager *Manager, content, ip, owner string) Job {
	t.Helper()
	job, err := manager.Submit(context.Background(), SubmitInput{
		InputPath:    createInput(t, content),
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

func testInspectionReport() InspectionReport {
	previous := 7
	current := 9
	missing := 8
	return InspectionReport{
		Version:      "1",
		ChapterCount: 9,
		Issues: []InspectionIssue{{
			Type:            "missing_chapter",
			Severity:        "warning",
			LineNumber:      120,
			ChapterTitle:    "第9章",
			Message:         "疑似缺少第8章",
			Evidence:        "第7章 -> 第9章",
			Detector:        "chapter_sequence",
			PreviousChapter: &previous,
			CurrentChapter:  &current,
			MissingFrom:     &missing,
			MissingTo:       &missing,
		}},
	}
}
