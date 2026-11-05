package goepub

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"log"
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

// TestEPUBConverterConvertDoesNotLogMissingParentSection 验证 Windows 下嵌套目录不会触发
// 第三方库 TOC 父节点路径不匹配的告警日志。
func TestEPUBConverterConvertDoesNotLogMissingParentSection(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "nested.txt")
	outputDir := filepath.Join(tmpDir, "output")

	content := strings.Join([]string{
		"目录告警回归测试",
		"作者：王五",
		"楔子",
		"卷前内容",
		"第一卷 起点",
		"第一章 开始",
		"第一段内容",
		"第二章 继续",
		"第二段内容",
		"第二卷 新的开始",
		"第三章 风 & 雨",
		"第三段内容",
	}, "\n")

	if err := os.WriteFile(txtPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	var logBuffer bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logBuffer)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	book := &Book{
		Filename: txtPath,
		Output:   outputDir,
	}
	if err := NewEPUBConverter().Convert(context.Background(), book); err != nil {
		t.Fatalf("convert: %v", err)
	}

	if got := logBuffer.String(); strings.Contains(got, "parent section not found") {
		t.Fatalf("unexpected toc warning log: %s", got)
	}

	outputPath, err := book.OutputPath()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	type navItem struct {
		Link struct {
			Title string `xml:",chardata"`
			Href  string `xml:"href,attr"`
		} `xml:"a"`
		Children []navItem `xml:"ol>li"`
	}
	type ncxItem struct {
		Title string `xml:"navLabel>text"`
		Link  struct {
			Href string `xml:"src,attr"`
		} `xml:"content"`
		Children []ncxItem `xml:"navPoint"`
	}
	var nav struct {
		Items []navItem `xml:"body>nav>ol>li"`
	}
	var ncx struct {
		Items []ncxItem `xml:"navMap>navPoint"`
	}
	for _, doc := range []struct {
		name   string
		target any
	}{
		{"EPUB/nav.xhtml", &nav},
		{"EPUB/toc.ncx", &ncx},
	} {
		file, err := reader.Open(doc.name)
		if err != nil {
			t.Fatal(err)
		}
		err = xml.NewDecoder(file).Decode(doc.target)
		file.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", doc.name, err)
		}
	}
	if len(nav.Items) != 3 || len(ncx.Items) != 3 {
		t.Fatalf("expected 3 root entries, nav=%d ncx=%d", len(nav.Items), len(ncx.Items))
	}
	want := []struct {
		title    string
		href     string
		chapters []string
	}{
		{"楔子", "xhtml/volume0_chapter0.xhtml", nil},
		{"第一卷 起点", "xhtml/volume1.xhtml", []string{"第一章 开始", "第二章 继续"}},
		{"第二卷 新的开始", "xhtml/volume2.xhtml", []string{"第三章 风 & 雨"}},
	}
	for i, expected := range want {
		n, c := nav.Items[i], ncx.Items[i]
		if n.Link.Title != expected.title || c.Title != expected.title || n.Link.Href != expected.href || c.Link.Href != expected.href {
			t.Fatalf("root %d: unexpected title or link: nav=%+v ncx=%+v", i, n, c)
		}
		if len(n.Children) != len(expected.chapters) || len(c.Children) != len(expected.chapters) {
			t.Fatalf("root %d: missing chapters: nav=%d ncx=%d want=%d", i, len(n.Children), len(c.Children), len(expected.chapters))
		}
		for j, title := range expected.chapters {
			chapterHref := fmt.Sprintf("xhtml/volume%d_chapter%d.xhtml", i, j)
			if n.Children[j].Link.Title != title || c.Children[j].Title != title || n.Children[j].Link.Href != chapterHref || c.Children[j].Link.Href != chapterHref {
				t.Fatalf("root %d chapter %d: unexpected title or link", i, j)
			}
			if file, err := reader.Open("EPUB/" + chapterHref); err != nil {
				t.Fatalf("chapter target missing: %v", err)
			} else {
				file.Close()
			}
		}
	}
}
