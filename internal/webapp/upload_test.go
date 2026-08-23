package webapp

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func TestParseUpload(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		content     []byte
		coverURL    string
		maxBytes    int64
		wantErr     error
		wantMessage string
		wantDisplay string
		wantText    string
	}{
		{name: "有效TXT", filename: "小说.txt", content: []byte("第一章 开始\n正文"), coverURL: "https://example.com/a.jpg", maxBytes: 1024, wantDisplay: "小说.txt"},
		{name: "有效UTF8BOM", filename: "novel.txt", content: append([]byte{0xEF, 0xBB, 0xBF}, []byte("第一章 开始\n正文")...), maxBytes: 1024, wantDisplay: "novel.txt", wantText: "第一章 开始\n正文"},
		{name: "清理展示文件名", filename: "../bad\r\nname.txt", content: []byte("第一章"), maxBytes: 1024, wantDisplay: "badname.txt"},
		{name: "拒绝扩展名", filename: "novel.zip", content: []byte("text"), maxBytes: 1024, wantErr: errInvalidUpload},
		{name: "拒绝二进制", filename: "novel.txt", content: []byte{'a', 0, 'b'}, maxBytes: 1024, wantErr: errInvalidUpload},
		{name: "自动转换UTF16LE", filename: "novel.txt", content: utf16Text(t, "第一章 开始\n正文", binary.LittleEndian), maxBytes: 1024, wantDisplay: "novel.txt", wantText: "第一章 开始\n正文"},
		{name: "自动转换UTF16BE", filename: "novel.txt", content: utf16Text(t, "第一章 开始\n正文", binary.BigEndian), maxBytes: 1024, wantDisplay: "novel.txt", wantText: "第一章 开始\n正文"},
		{name: "H5无BOMUTF16LE", filename: "父可敌国.txt", content: utf16Text(t, "父可敌国\n第一章 开始\n正文😀", binary.LittleEndian)[2:], maxBytes: 1024, wantDisplay: "父可敌国.txt", wantText: "父可敌国\n第一章 开始\n正文😀"},
		{name: "H5无BOMUTF16BE", filename: "父可敌国.txt", content: utf16Text(t, "父可敌国\n第一章 开始\n正文😀", binary.BigEndian)[2:], maxBytes: 1024, wantDisplay: "父可敌国.txt", wantText: "父可敌国\n第一章 开始\n正文😀"},
		{name: "自动转换GB18030", filename: "novel.txt", content: gb18030Text(t, "第一章 开始\n正文"), maxBytes: 1024, wantDisplay: "novel.txt", wantText: "第一章 开始\n正文"},
		{name: "自动转换CP936欧元符号", filename: "novel.txt", content: []byte{'a', 0x80, 'b'}, maxBytes: 1024, wantDisplay: "novel.txt", wantText: "a€b"},
		{name: "拒绝损坏UTF16", filename: "novel.txt", content: []byte{0xFF, 0xFE, 0x00, 0xD8}, maxBytes: 1024, wantErr: errInvalidUpload},
		{name: "拒绝无法可靠识别编码", filename: "novel.txt", content: []byte{0x81, 0x30, 0x81}, maxBytes: 1024, wantErr: errInvalidUpload, wantMessage: "另存为 UTF-8 后重试"},
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
				if tt.wantMessage != "" && !strings.Contains(err.Error(), tt.wantMessage) {
					t.Fatalf("parseUpload() error = %q, want message containing %q", err, tt.wantMessage)
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
			stored, readErr := os.ReadFile(got.InputPath)
			if readErr != nil {
				t.Fatalf("read normalized input: %v", readErr)
			}
			if !utf8.Valid(stored) || bytes.IndexByte(stored, 0) >= 0 {
				t.Fatalf("stored input is not UTF-8 text: %x", stored)
			}
			if got.InputSize != int64(len(stored)) {
				t.Fatalf("InputSize = %d, want stored size %d", got.InputSize, len(stored))
			}
			if tt.wantText != "" && string(stored) != tt.wantText {
				t.Fatalf("stored input = %q, want %q", stored, tt.wantText)
			}
			if got.CoverURL != tt.coverURL {
				t.Fatalf("CoverURL = %q, want %q", got.CoverURL, tt.coverURL)
			}
		})
	}
}

func TestParseUploadReportsInterruptedStream(t *testing.T) {
	body, contentType := multipartTXT(t, "novel.txt", "第一章 开始\n正文")
	raw := body.Bytes()
	if len(raw) < 8 {
		t.Fatal("multipart fixture is unexpectedly short")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/conversions", bytes.NewReader(raw[:len(raw)-8]))
	req.Header.Set("Content-Type", contentType)
	incoming := filepath.Join(t.TempDir(), "incoming")

	_, err := parseUpload(httptest.NewRecorder(), req, incoming, 1024, 1024)
	if !errors.Is(err, errInvalidUpload) || !errors.Is(err, errUploadInterrupted) {
		t.Fatalf("parseUpload() error = %v, want interrupted upload error", err)
	}
	entries, readErr := os.ReadDir(incoming)
	if readErr == nil && len(entries) != 0 {
		t.Fatalf("interrupted upload left files: %v", entries)
	}
}

func utf16Text(t *testing.T, text string, order binary.ByteOrder) []byte {
	t.Helper()
	units := utf16.Encode([]rune(text))
	output := make([]byte, 2+len(units)*2)
	if order == binary.LittleEndian {
		output[0], output[1] = 0xFF, 0xFE
	} else {
		output[0], output[1] = 0xFE, 0xFF
	}
	for i, unit := range units {
		order.PutUint16(output[2+i*2:], unit)
	}
	return output
}

func gb18030Text(t *testing.T, text string) []byte {
	t.Helper()
	encoded, _, err := transform.String(simplifiedchinese.GB18030.NewEncoder(), text)
	if err != nil {
		t.Fatalf("encode GB18030: %v", err)
	}
	return []byte(encoded)
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

func TestParseUploadDiscardsLegacyBrowserAutoCover(t *testing.T) {
	validPNG, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScL1XQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	textPart, err := writer.CreateFormFile("file", "novel.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := textPart.Write([]byte("测试小说\n作者：测试作者\n第一章 开始\n正文")); err != nil {
		t.Fatal(err)
	}
	coverPart, err := writer.CreateFormFile("cover_file", legacyAutoCoverName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coverPart.Write(validPNG); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	incoming := filepath.Join(t.TempDir(), "incoming")
	req := httptest.NewRequest(http.MethodPost, "/api/conversions", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	got, err := parseUpload(httptest.NewRecorder(), req, incoming, 1024, 1024)
	if err != nil {
		t.Fatalf("parseUpload() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(got.InputPath) })
	if !got.LegacyAutoCoverDiscarded {
		t.Fatal("legacy browser auto cover was not marked as discarded")
	}
	if got.CoverPath != "" {
		t.Fatalf("CoverPath = %q, want empty path for regenerated cover", got.CoverPath)
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
