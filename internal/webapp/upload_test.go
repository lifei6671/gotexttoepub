package webapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseUpload(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		content     []byte
		coverURL    string
		maxBytes    int64
		wantErr     error
		wantDisplay string
	}{
		{name: "有效TXT", filename: "小说.txt", content: []byte("第一章 开始\n正文"), coverURL: "https://example.com/a.jpg", maxBytes: 1024, wantDisplay: "小说.txt"},
		{name: "清理展示文件名", filename: "../bad\r\nname.txt", content: []byte("第一章"), maxBytes: 1024, wantDisplay: "badname.txt"},
		{name: "拒绝扩展名", filename: "novel.zip", content: []byte("text"), maxBytes: 1024, wantErr: errInvalidUpload},
		{name: "拒绝二进制", filename: "novel.txt", content: []byte{'a', 0, 'b'}, maxBytes: 1024, wantErr: errInvalidUpload},
		{name: "拒绝过大", filename: "novel.txt", content: []byte("12345"), maxBytes: 4, wantErr: errUploadTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			filePart, err := writer.CreateFormFile("file", tt.filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := filePart.Write(tt.content); err != nil {
				t.Fatal(err)
			}
			if tt.coverURL != "" {
				if err := writer.WriteField("cover_url", tt.coverURL); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest("POST", "/api/conversions", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			rec := httptest.NewRecorder()
			incoming := filepath.Join(t.TempDir(), "incoming")
			got, err := parseUpload(rec, req, incoming, tt.maxBytes, 1024)
			if tt.wantErr != nil {
				if err == nil || !errors.Is(err, tt.wantErr) {
					t.Fatalf("parseUpload() error = %v, want %v", err, tt.wantErr)
				}
				entries, readErr := os.ReadDir(incoming)
				if readErr == nil && len(entries) != 0 {
					t.Fatalf("failed upload left files: %v", entries)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUpload() error = %v", err)
			}
			t.Cleanup(func() { _ = os.Remove(got.InputPath) })
			if got.OriginalName != tt.wantDisplay {
				t.Fatalf("OriginalName = %q, want %q", got.OriginalName, tt.wantDisplay)
			}
			if got.InputSize != int64(len(tt.content)) {
				t.Fatalf("InputSize = %d, want %d", got.InputSize, len(tt.content))
			}
			if got.CoverURL != tt.coverURL {
				t.Fatalf("CoverURL = %q, want %q", got.CoverURL, tt.coverURL)
			}
		})
	}
}

func TestParseUploadRejectsUnknownField(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("unexpected", "value"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	_, err := parseUpload(httptest.NewRecorder(), req, t.TempDir(), 1024, 1024)
	if err == nil || !strings.Contains(err.Error(), "不支持字段") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestParseUploadCoverFile(t *testing.T) {
	validPNG, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScL1XQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		coverName  string
		cover      []byte
		coverURL   string
		maxCover   int64
		wantErr    error
		wantSuffix string
	}{
		{name: "接受真实PNG", coverName: "cover.png", cover: validPNG, maxCover: 1024, wantSuffix: ".png"},
		{name: "接受真实JPEG", coverName: "cover.jpeg", cover: testJPEG(t), maxCover: 1024, wantSuffix: ".jpg"},
		{name: "拒绝伪造图片", coverName: "cover.jpg", cover: []byte("not an image"), maxCover: 1024, wantErr: errInvalidCover},
		{name: "拒绝过大图片", coverName: "cover.png", cover: validPNG, maxCover: 5, wantErr: errCoverTooLarge},
		{name: "链接和上传互斥", coverName: "cover.png", cover: validPNG, coverURL: "https://example.com/cover.jpg", maxCover: 1024, wantErr: errInvalidUpload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			textPart, createErr := writer.CreateFormFile("file", "novel.txt")
			if createErr != nil {
				t.Fatal(createErr)
			}
			if _, createErr = textPart.Write([]byte("书名\n第一章\n正文")); createErr != nil {
				t.Fatal(createErr)
			}
			coverPart, createErr := writer.CreateFormFile("cover_file", tt.coverName)
			if createErr != nil {
				t.Fatal(createErr)
			}
			if _, createErr = coverPart.Write(tt.cover); createErr != nil {
				t.Fatal(createErr)
			}
			if tt.coverURL != "" {
				if createErr = writer.WriteField("cover_url", tt.coverURL); createErr != nil {
					t.Fatal(createErr)
				}
			}
			if createErr = writer.Close(); createErr != nil {
				t.Fatal(createErr)
			}

			incoming := filepath.Join(t.TempDir(), "incoming")
			req := httptest.NewRequest("POST", "/api/conversions", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			got, parseErr := parseUpload(httptest.NewRecorder(), req, incoming, 1024, tt.maxCover)
			if tt.wantErr != nil {
				if !errors.Is(parseErr, tt.wantErr) {
					t.Fatalf("parseUpload() error = %v, want %v", parseErr, tt.wantErr)
				}
				entries, readErr := os.ReadDir(incoming)
				if readErr == nil && len(entries) != 0 {
					t.Fatalf("failed upload left files: %v", entries)
				}
				return
			}
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			t.Cleanup(func() {
				_ = os.Remove(got.InputPath)
				_ = os.Remove(got.CoverPath)
			})
			if !strings.HasSuffix(got.CoverPath, tt.wantSuffix) {
				t.Fatalf("CoverPath = %q, want suffix %q", got.CoverPath, tt.wantSuffix)
			}
			info, statErr := os.Stat(got.CoverPath)
			if statErr != nil || !info.Mode().IsRegular() {
				t.Fatalf("cover file is unavailable: %v", statErr)
			}
		})
	}
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 1, 1))
	canvas.Set(0, 0, color.RGBA{R: 180, G: 40, B: 40, A: 255})
	var output bytes.Buffer
	if err := jpeg.Encode(&output, canvas, nil); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
