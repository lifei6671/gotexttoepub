package webapp

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"github.com/lifei6671/gotexttoepub/internal/jobs"
)

type rejectingCoverFetcher struct{}

func (rejectingCoverFetcher) Fetch(context.Context, string, string) (string, error) {
	return "", context.Canceled
}

func TestServerConversionFlow(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := jobs.NewManager(jobs.Config{
		DataDir:         dataDir,
		Workers:         1,
		QueueSize:       1,
		PerIPLimit:      1,
		Retention:       time.Hour,
		CleanupInterval: time.Hour,
		MaxDiskBytes:    10 << 20,
		Convert:         fakeConvert,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	server, err := NewServer(Config{
		Assets: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>test</title>")},
			"app.css":    &fstest.MapFile{Data: []byte("body{}")},
			"app.js":     &fstest.MapFile{Data: []byte(`"use strict";`)},
		},
		Manager:        manager,
		CoverFetcher:   rejectingCoverFetcher{},
		DataDir:        dataDir,
		MaxUploadBytes: 1024,
		MaxCoverBytes:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	body, contentType := multipartTXT(t, "测试.txt", "测试小说\n作者：甲\n第一章 开始\n正文")
	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/conversions", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("submit status = %d, body = %s", response.StatusCode, raw)
	}
	var submitted responseJob
	if err := json.NewDecoder(response.Body).Decode(&submitted); err != nil {
		t.Fatal(err)
	}
	if !validJobID(submitted.ID) {
		t.Fatalf("invalid job id %q", submitted.ID)
	}
	cookies := response.Cookies()
	if len(cookies) == 0 || cookies[0].Name != clientCookieName || !cookies[0].HttpOnly {
		t.Fatalf("missing secure client cookie: %#v", cookies)
	}

	var completed responseJob
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		statusRequest, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/conversions/"+submitted.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		statusRequest.AddCookie(cookies[0])
		statusResponse, err := http.DefaultClient.Do(statusRequest)
		if err != nil {
			t.Fatal(err)
		}
		if statusResponse.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(statusResponse.Body)
			_ = statusResponse.Body.Close()
			t.Fatalf("status = %d, body = %s", statusResponse.StatusCode, raw)
		}
		if err := json.NewDecoder(statusResponse.Body).Decode(&completed); err != nil {
			_ = statusResponse.Body.Close()
			t.Fatal(err)
		}
		_ = statusResponse.Body.Close()
		if completed.Status == string(jobs.StatusSucceeded) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if completed.Status != string(jobs.StatusSucceeded) {
		t.Fatalf("job did not complete: %#v", completed)
	}
	if completed.OutputName != "测试.epub" || completed.OutputSize == 0 || completed.ExpiresAt == nil {
		t.Fatalf("unexpected completed job: %#v", completed)
	}

	downloadRequest, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/conversions/"+submitted.ID+"/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	downloadRequest.AddCookie(cookies[0])
	downloadResponse, err := http.DefaultClient.Do(downloadRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer downloadResponse.Body.Close()
	if downloadResponse.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", downloadResponse.StatusCode)
	}
	if got := downloadResponse.Header.Get("Content-Type"); got != "application/epub+zip" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := downloadResponse.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	raw, err := io.ReadAll(downloadResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("downloaded EPUB is empty")
	}
}

func TestServerStaticAssetsAreNotCached(t *testing.T) {
	server := newReviewServer(t)
	for _, path := range []string{"/", "/app.css", "/app.js"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("GET %s Cache-Control = %q, want no-store", path, got)
		}
	}
}

func TestServerReportsInterruptedUpload(t *testing.T) {
	server := newReviewServer(t)
	body, contentType := multipartTXT(t, "novel.txt", "第一章 开始\n正文")
	raw := body.Bytes()
	request := httptest.NewRequest(http.MethodPost, "/api/conversions", bytes.NewReader(raw[:len(raw)-8]))
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "UPLOAD_INTERRUPTED" || !strings.Contains(response.Error.Message, "上传未完成") {
		t.Fatalf("response = %#v, want retryable interrupted-upload error", response.Error)
	}
}

// iPhone Safari 与其他浏览器都会经 FormData 提交同样的 multipart 内容。
func TestServerAcceptsBrowserMultipartNoBOMUTF16(t *testing.T) {
	dataDir := t.TempDir()
	convertedText := make(chan []byte, 1)
	manager, err := jobs.NewManager(jobs.Config{
		DataDir:         dataDir,
		Workers:         1,
		QueueSize:       1,
		PerIPLimit:      1,
		Retention:       time.Hour,
		CleanupInterval: time.Hour,
		MaxDiskBytes:    10 << 20,
		Convert: func(ctx context.Context, job *jobs.Job, inputPath, coverPath string) (string, int64, error) {
			text, readErr := os.ReadFile(inputPath)
			if readErr != nil {
				return "", 0, readErr
			}
			convertedText <- text
			return fakeConvert(ctx, job, inputPath, coverPath)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := manager.Start(ctx); err != nil {
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

	wantText := "父可敌国\n第一章 开始\n正文😀"
	body, contentType := multipartTXTBytes(t, "父可敌国.txt", utf16Text(t, wantText, binary.LittleEndian)[2:])
	request := httptest.NewRequest(http.MethodPost, "/api/conversions", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	select {
	case got := <-convertedText:
		if string(got) != wantText || !utf8.Valid(got) || bytes.IndexByte(got, 0) >= 0 {
			t.Fatalf("converter input = %q, want UTF-8 %q", got, wantText)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("browser multipart upload did not reach converter")
	}
}

func TestServerRejectsUnrecognizedBrowserUploadWithActionableError(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := jobs.NewManager(jobs.Config{
		DataDir:         dataDir,
		Workers:         1,
		QueueSize:       1,
		PerIPLimit:      1,
		Retention:       time.Hour,
		CleanupInterval: time.Hour,
		MaxDiskBytes:    10 << 20,
		Convert:         fakeConvert,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := manager.Start(ctx); err != nil {
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

	body, contentType := multipartTXTBytes(t, "novel.txt", []byte{0x81, 0x30, 0x81})
	request := httptest.NewRequest(http.MethodPost, "/api/conversions", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("submit status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "INVALID_UPLOAD" || !strings.Contains(response.Error.Message, "另存为 UTF-8 后重试") {
		t.Fatalf("error response = %#v, want actionable INVALID_UPLOAD", response.Error)
	}
}

func TestServerBindsJobsToBrowserCookie(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := jobs.NewManager(jobs.Config{
		DataDir:         dataDir,
		Workers:         1,
		QueueSize:       1,
		PerIPLimit:      1,
		Retention:       time.Hour,
		CleanupInterval: time.Hour,
		Convert:         fakeConvert,
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

	body, contentType := multipartTXT(t, "test.txt", "书名\n第一章\n正文")
	request := httptest.NewRequest(http.MethodPost, "/api/conversions", body)
	request.RemoteAddr = "192.0.2.1:1000"
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var submitted responseJob
	if err := json.Unmarshal(recorder.Body.Bytes(), &submitted); err != nil {
		t.Fatal(err)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/conversions/"+submitted.ID, nil)
	statusRequest.RemoteAddr = "192.0.2.1:1001"
	statusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusNotFound {
		t.Fatalf("unbound status request = %d, want 404", statusRecorder.Code)
	}
}

func TestServerUsesUploadedCoverWithoutRemoteFetch(t *testing.T) {
	dataDir := t.TempDir()
	coverReceived := make(chan []byte, 1)
	manager, err := jobs.NewManager(jobs.Config{
		DataDir:         dataDir,
		Workers:         1,
		QueueSize:       1,
		PerIPLimit:      1,
		Retention:       time.Hour,
		CleanupInterval: time.Hour,
		Convert: func(ctx context.Context, job *jobs.Job, inputPath, coverPath string) (string, int64, error) {
			if coverPath == "" {
				return "", 0, errors.New("uploaded cover was not passed to converter")
			}
			cover, readErr := os.ReadFile(coverPath)
			if readErr != nil {
				return "", 0, readErr
			}
			coverReceived <- cover
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

	cover, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScL1XQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	textPart, err := writer.CreateFormFile("file", "book.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = textPart.Write([]byte("书名\n第一章\n正文")); err != nil {
		t.Fatal(err)
	}
	coverPart, err := writer.CreateFormFile("cover_file", "cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = coverPart.Write(cover); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/conversions", &body)
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	select {
	case got := <-coverReceived:
		if !bytes.Equal(got, cover) {
			t.Fatal("converter received a different cover payload")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("converter did not receive uploaded cover")
	}
}

func TestServerSetsBrowserSecurityHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := &Server{logger: testLogger(), handler: nil}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	server.middleware(handler).ServeHTTP(recorder, request)
	for _, header := range []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"Permissions-Policy",
	} {
		if recorder.Header().Get(header) == "" {
			t.Fatalf("missing security header %s", header)
		}
	}
	if policy := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "img-src 'self' data: blob:") {
		t.Fatalf("CSP does not allow local cover previews: %q", policy)
	}
}

func TestServerLogsAPIError(t *testing.T) {
	var logs bytes.Buffer
	server := &Server{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIError(w, http.StatusBadRequest, "INVALID_UPLOAD", "无法识别 TXT 编码", 0)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/conversions", nil)
	recorder := httptest.NewRecorder()
	server.middleware(handler).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(logs.String(), `"msg":"API 请求失败"`) || !strings.Contains(logs.String(), `"error_code":"INVALID_UPLOAD"`) {
		t.Fatalf("API error was not logged: %s", logs.String())
	}
}

func TestSameOriginRequestChecksSchemeAndConfiguredPublicOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://example.test/api/conversions", nil)
	request.Header.Set("Origin", "https://example.test")
	if (&Server{}).sameOriginRequest(request) {
		t.Fatal("same host with a different scheme must be rejected")
	}

	publicOrigin, err := normalizeOrigin("https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{publicOrigin: publicOrigin}
	if !server.sameOriginRequest(request) {
		t.Fatal("configured HTTPS public origin should be accepted behind a proxy")
	}

	request.Header.Set("Origin", "https://other.test")
	if server.sameOriginRequest(request) {
		t.Fatal("different public origin must be rejected")
	}
}

func TestNormalizeOriginRejectsCredentialsAndPaths(t *testing.T) {
	for _, raw := range []string{
		"ftp://example.test",
		"https://user@example.test",
		"https://example.test/path",
		"https://example.test?query=1",
	} {
		if _, err := normalizeOrigin(raw); err == nil {
			t.Fatalf("normalizeOrigin(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestConvertEPUBWritesFixedPrivateOutput(t *testing.T) {
	jobDir := t.TempDir()
	inputPath := filepath.Join(jobDir, "input.txt")
	content := "转换测试\n作者：测试者\n第一章 开始\n正文内容"
	if err := os.WriteFile(inputPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	name, size, err := ConvertEPUB(context.Background(), &jobs.Job{}, inputPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "转换测试.epub" {
		t.Fatalf("output display name = %q", name)
	}
	outputPath := filepath.Join(jobDir, "output.epub")
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != size {
		t.Fatalf("reported size %d != stat size %d", size, info.Size())
	}
	reader, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) == 0 {
		t.Fatal("EPUB zip contains no entries")
	}
	var coverPNG []byte
	for _, entry := range reader.File {
		if !strings.HasSuffix(entry.Name, ".png") {
			continue
		}
		file, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		coverPNG = content
		break
	}
	if len(coverPNG) == 0 {
		t.Fatal("EPUB does not contain generated PNG cover")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(coverPNG))
	if err != nil {
		t.Fatalf("decode generated EPUB cover: %v", err)
	}
	if format != "png" || config.Width != autoCoverWidth || config.Height != autoCoverHeight {
		t.Fatalf("generated EPUB cover = %s %dx%d, want png %dx%d", format, config.Width, config.Height, autoCoverWidth, autoCoverHeight)
	}
}

func fakeConvert(_ context.Context, _ *jobs.Job, inputPath, _ string) (string, int64, error) {
	outputPath := filepath.Join(filepath.Dir(inputPath), "output.epub")
	file, err := os.Create(outputPath)
	if err != nil {
		return "", 0, err
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create("mimetype")
	if err == nil {
		_, err = entry.Write([]byte("application/epub+zip"))
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", 0, err
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return "", 0, err
	}
	return "测试.epub", info.Size(), nil
}

func multipartTXT(t *testing.T, filename, content string) (*bytes.Buffer, string) {
	return multipartTXTBytes(t, filename, []byte(content))
}

func multipartTXTBytes(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")},
		"app.css":    &fstest.MapFile{Data: []byte{}},
		"app.js":     &fstest.MapFile{Data: []byte{}},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
