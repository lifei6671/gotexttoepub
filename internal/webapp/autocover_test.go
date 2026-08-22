package webapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildAutoCoverPNG(t *testing.T) {
	pngData, err := buildAutoCoverPNG("测试标题", "测试作者")
	if err != nil {
		t.Fatalf("buildAutoCoverPNG() error = %v", err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("decode generated cover: %v", err)
	}
	if format != "png" {
		t.Fatalf("generated cover format = %q, want png", format)
	}
	if config.Width != autoCoverWidth || config.Height != autoCoverHeight {
		t.Fatalf("generated cover size = %dx%d, want %dx%d", config.Width, config.Height, autoCoverWidth, autoCoverHeight)
	}
}

func TestBuildAutoCoverPNGChangesWhenAuthorChanges(t *testing.T) {
	first, err := buildAutoCoverPNG("测试小说", "甲作者")
	if err != nil {
		t.Fatalf("buildAutoCoverPNG() first error = %v", err)
	}
	second, err := buildAutoCoverPNG("测试小说", "乙作者")
	if err != nil {
		t.Fatalf("buildAutoCoverPNG() second error = %v", err)
	}
	if sha256.Sum256(first) == sha256.Sum256(second) {
		t.Fatal("different authors produced identical cover PNGs")
	}
}

func TestAutoCoverTitleFontSizeFitsTwoLineTitle(t *testing.T) {
	lines := autoCoverTitleLines("一二三四五六七八九十一二三四五六七八九十")
	size := autoCoverTitleFontSize(lines)
	if size >= 86 {
		t.Fatalf("long title font size = %v, want smaller than 86", size)
	}
	if size*float64(utf8.RuneCountInString(lines[0])) > autoCoverTitleWidth {
		t.Fatalf("first title line exceeds width: size=%v line=%q", size, lines[0])
	}
	if size*float64(utf8.RuneCountInString(lines[1])) > autoCoverTitleWidth {
		t.Fatalf("second title line exceeds width: size=%v line=%q", size, lines[1])
	}
}

func TestAutoCoverGeneratorCreatesCleanedUpPNG(t *testing.T) {
	generator := newAutoCoverGenerator(t.TempDir())
	path, cleanup, err := generator(context.Background(), "测试小说", "测试作者")
	if err != nil {
		t.Fatalf("generator() error = %v", err)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Fatalf("generated path = %q, want .png", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated PNG: %v", err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(content)); err != nil || format != "png" {
		t.Fatalf("generated file is not a PNG: format=%q error=%v", format, err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("generated SVG stat error = %v, want not exist", err)
	}
}
