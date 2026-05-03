package goepub

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEPUBConverterConvertSupportsLongLines 验证超长正文行不会触发 scanner 长度限制。
func TestEPUBConverterConvertSupportsLongLines(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "story.txt")
	outputDir := filepath.Join(tmpDir, "output")

	content := strings.Join([]string{
		"测试小说",
		"作者：张三",
		"第一卷 初见",
		"第一章 开始",
		strings.Repeat("这是一段很长的正文。", 6000),
		"第二章 继续",
		"新的正文段落",
	}, "\n")

	if err := os.WriteFile(txtPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	book := &Book{
		Filename: txtPath,
		Output:   outputDir,
	}
	if err := NewEPUBConverter().Convert(context.Background(), book); err != nil {
		t.Fatalf("convert: %v", err)
	}

	outputPath := filepath.Join(outputDir, "测试小说.epub")
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected epub output: %v", err)
	}

	reader, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("open epub: %v", err)
	}
	defer reader.Close()

	if len(reader.File) == 0 {
		t.Fatal("expected generated epub to contain files")
	}
}

// TestLegacyConverterRemainsUsable 验证旧版链式 API 在重构后仍可正常使用。
func TestLegacyConverterRemainsUsable(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "legacy.txt")
	outputPath := filepath.Join(tmpDir, "legacy.epub")
	rules, err := compileRuleConfig(defaultRuleConfig())
	if err != nil {
		t.Fatalf("compile default rules: %v", err)
	}

	content := strings.Join([]string{
		"旧版兼容测试",
		"作者：李四",
		"第一章 起点",
		"第一段内容",
	}, "\n")

	if err := os.WriteFile(txtPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	if err := NewConverter().
		SetContent(txtPath).
		SetRegExp(rules.ChapterRegex).
		Convert(outputPath); err != nil {
		t.Fatalf("legacy convert: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected legacy epub output: %v", err)
	}
}
