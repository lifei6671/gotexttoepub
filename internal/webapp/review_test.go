package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lifei6671/gotexttoepub/goepub"
	"github.com/lifei6671/gotexttoepub/internal/jobs"
)

func TestConvertEPUBMapsPreflightReportAndAllowsConfirmedConversion(t *testing.T) {
	jobDir := t.TempDir()
	inputPath := filepath.Join(jobDir, "input.txt")
	content := "检测测试\n作者：测试者\n第一章 开始\n正文\n第三章 跳章\n正文"
	if err := os.WriteFile(inputPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := ConvertEPUB(context.Background(), &jobs.Job{}, inputPath, "")
	var reviewErr *jobs.ReviewRequiredError
	if !errors.As(err, &reviewErr) || reviewErr == nil {
		t.Fatalf("ConvertEPUB() error = %v, want *jobs.ReviewRequiredError", err)
	}
	if len(reviewErr.Report.Issues) != 1 ||
		reviewErr.Report.Issues[0].Type != goepub.PreflightIssueMissingChapter ||
		reviewErr.Report.Issues[0].MissingFrom == nil ||
		*reviewErr.Report.Issues[0].MissingFrom != 2 {
		t.Fatalf("mapped inspection report = %+v", reviewErr.Report)
	}
	if _, statErr := os.Stat(filepath.Join(jobDir, "output.epub")); !os.IsNotExist(statErr) {
		t.Fatalf("blocked conversion output stat error = %v, want not exist", statErr)
	}

	name, size, err := ConvertEPUB(context.Background(), &jobs.Job{AllowAnomalies: true}, inputPath, "")
	if err != nil {
		t.Fatalf("confirmed ConvertEPUB() error = %v", err)
	}
	if name != "检测测试.epub" || size <= 0 {
		t.Fatalf("confirmed output = %q, %d", name, size)
	}
}

func TestServerReviewReportAndContinue(t *testing.T) {
	server := newReviewServer(t)
	submitted, cookie := submitReviewJob(t, server, "http://example.test", "192.0.2.70:1000")
	review := waitForWebStatus(t, server, submitted.ID, cookie, jobs.StatusReviewRequired)
	if review.Inspection == nil || len(review.Inspection.Issues) != 1 || review.ReviewExpiresAt == nil {
		t.Fatalf("review response = %+v", review)
	}

	rejected := reviewRequest(t, server, http.MethodPost,
		"http://example.test/api/conversions/"+submitted.ID+"/continue", cookie, "http://evil.test")
	if rejected.Code != http.StatusForbidden {
		t.Fatalf("cross-origin continue status = %d, want 403", rejected.Code)
	}

	continued := reviewRequest(t, server, http.MethodPost,
		"http://example.test/api/conversions/"+submitted.ID+"/continue", cookie, "http://example.test")
	if continued.Code != http.StatusAccepted {
		t.Fatalf("continue status = %d, body = %s", continued.Code, continued.Body.String())
	}
	waitForWebStatus(t, server, submitted.ID, cookie, jobs.StatusSucceeded)
}

func TestServerDiscardReviewRequiresOriginAndOwner(t *testing.T) {
	server := newReviewServer(t)
	submitted, cookie := submitReviewJob(t, server, "http://example.test", "192.0.2.71:1000")
	waitForWebStatus(t, server, submitted.ID, cookie, jobs.StatusReviewRequired)

	wrongOwner := reviewRequest(t, server, http.MethodDelete,
		"http://example.test/api/conversions/"+submitted.ID, nil, "http://example.test")
	if wrongOwner.Code != http.StatusNotFound {
		t.Fatalf("unowned discard status = %d, want 404", wrongOwner.Code)
	}
	crossOrigin := reviewRequest(t, server, http.MethodDelete,
		"http://example.test/api/conversions/"+submitted.ID, cookie, "http://evil.test")
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin discard status = %d, want 403", crossOrigin.Code)
	}

	discarded := reviewRequest(t, server, http.MethodDelete,
		"http://example.test/api/conversions/"+submitted.ID, cookie, "http://example.test")
	if discarded.Code != http.StatusNoContent {
		t.Fatalf("discard status = %d, body = %s", discarded.Code, discarded.Body.String())
	}
	missing := reviewRequest(t, server, http.MethodGet,
		"http://example.test/api/conversions/"+submitted.ID, cookie, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("discarded job status = %d, want 404", missing.Code)
	}
}

type webReviewResponse struct {
	ID              string                 `json:"id"`
	Status          string                 `json:"status"`
	Inspection      *jobs.InspectionReport `json:"inspection"`
	ReviewExpiresAt *time.Time             `json:"reviewExpiresAt"`
}

func newReviewServer(t *testing.T) *Server {
	t.Helper()
	dataDir := t.TempDir()
	report := jobs.InspectionReport{
		Version:      "1",
		ChapterCount: 2,
		Issues: []jobs.InspectionIssue{{
			Type:         goepub.PreflightIssueThirdPartyAd,
			Severity:     goepub.PreflightSeverityWarning,
			LineNumber:   12,
			ChapterTitle: "第一章 开始",
			Message:      "疑似第三方广告",
			Evidence:     "请访问 example.test",
			Detector:     "test_detector",
		}},
	}
	manager, err := jobs.NewManager(jobs.Config{
		DataDir:         dataDir,
		Workers:         1,
		QueueSize:       1,
		PerIPLimit:      1,
		Retention:       time.Hour,
		CleanupInterval: time.Hour,
		MaxDiskBytes:    10 << 20,
		Convert: func(ctx context.Context, job *jobs.Job, inputPath, coverPath string) (string, int64, error) {
			if !job.AllowAnomalies {
				return "", 0, &jobs.ReviewRequiredError{Report: report}
			}
			return fakeConvert(ctx, job, inputPath, coverPath)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	server, err := NewServer(Config{
		Assets:         testAssets(),
		Manager:        manager,
		CoverFetcher:   rejectingCoverFetcher{},
		DataDir:        dataDir,
		MaxUploadBytes: 1024,
		MaxCoverBytes:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func submitReviewJob(t *testing.T, server *Server, origin, remoteAddr string) (webReviewResponse, *http.Cookie) {
	t.Helper()
	body, contentType := multipartTXT(t, "review.txt", "测试小说\n第一章 开始\n正文")
	request := httptest.NewRequest(http.MethodPost, origin+"/api/conversions", body)
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Origin", origin)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var submitted webReviewResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &submitted); err != nil {
		t.Fatal(err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("submit response did not set owner cookie")
	}
	return submitted, cookies[0]
}

func waitForWebStatus(t *testing.T, server *Server, id string, cookie *http.Cookie, status jobs.Status) webReviewResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response := reviewRequest(t, server, http.MethodGet,
			"http://example.test/api/conversions/"+id, cookie, "")
		if response.Code != http.StatusOK {
			t.Fatalf("status request = %d, body = %s", response.Code, response.Body.String())
		}
		var job webReviewResponse
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if job.Status == string(status) {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", id, status)
	return webReviewResponse{}
}

func reviewRequest(t *testing.T, server *Server, method, target string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	request.RemoteAddr = "192.0.2.70:1001"
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}
