package goepub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightChapterSequence(t *testing.T) {
	tests := []struct {
		name         string
		lines        []string
		wantType     string
		wantChapters int
	}{
		{
			name:         "missing chapter",
			lines:        []string{"第一章 开始", "正文", "第三章 跳过了第二章", "正文"},
			wantType:     PreflightIssueMissingChapter,
			wantChapters: 2,
		},
		{
			name:         "duplicate chapter",
			lines:        []string{"第12章 开始", "正文", "第12章 重复", "正文"},
			wantType:     PreflightIssueDuplicateChapter,
			wantChapters: 2,
		},
		{
			name:         "out of order",
			lines:        []string{"第12章 开始", "正文", "第11章 倒序", "正文"},
			wantType:     PreflightIssueChapterOutOfOrder,
			wantChapters: 2,
		},
		{
			name: "volume reset from one is allowed",
			lines: []string{
				"第一卷 起点", "第九十九章 开始", "正文", "第一百章 继续", "正文",
				"第二卷 新篇", "第一章 重置", "正文", "第二章 继续", "正文",
			},
			wantChapters: 4,
		},
		{
			name: "continuous numbering across volumes",
			lines: []string{
				"第一卷 起点", "第九十九章 开始", "正文", "第一百章 继续", "正文",
				"第二卷 新篇", "第一百零一章 承接", "正文", "第一百零二章 继续", "正文",
			},
			wantChapters: 4,
		},
		{
			name: "missing chapter across volumes",
			lines: []string{
				"第一卷 起点", "第一百章 收尾", "正文",
				"第二卷 新篇", "第一百零二章 缺章", "正文",
			},
			wantType:     PreflightIssueMissingChapter,
			wantChapters: 2,
		},
		{
			name:         "chinese chapter numbers",
			lines:        []string{"第九十九章 开始", "正文", "第一百章 继续", "正文", "第一百零一章 继续", "正文"},
			wantChapters: 3,
		},
		{
			name:         "chinese positional digits",
			lines:        []string{"第二〇二章 开始", "正文", "第两百零三章 继续", "正文"},
			wantChapters: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := parsePreflightForTest(t, tt.lines)
			if report.ChapterCount != tt.wantChapters {
				t.Fatalf("ChapterCount = %d, want %d", report.ChapterCount, tt.wantChapters)
			}
			if tt.wantType == "" {
				if len(report.Issues) != 0 {
					t.Fatalf("unexpected issues: %+v", report.Issues)
				}
				return
			}
			if len(report.Issues) != 1 || report.Issues[0].Type != tt.wantType {
				t.Fatalf("issues = %+v, want one %s issue", report.Issues, tt.wantType)
			}
			if report.Issues[0].LineNumber == 0 || report.Issues[0].CurrentChapter == nil || report.Issues[0].PreviousChapter == nil {
				t.Fatalf("issue lacks location or sequence evidence: %+v", report.Issues[0])
			}
		})
	}
}

func TestParseChineseNumber(t *testing.T) {
	tests := map[string]int{
		"十":     10,
		"二十一":   21,
		"一百零二":  102,
		"二〇二":   202,
		"两百零三":  203,
		"一千零一十": 1010,
	}
	for input, want := range tests {
		got, ok := parseChineseNumber(input)
		if !ok || got != want {
			t.Errorf("parseChineseNumber(%q) = (%d, %v), want (%d, true)", input, got, ok, want)
		}
	}
}

func TestPreflightThirdPartyDetection(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantIssue bool
	}{
		{name: "website promotion", line: "更多精彩内容请访问 www.example.com 阅读最新章节", wantIssue: true},
		{name: "bare domain", line: "example.com", wantIssue: true},
		{name: "site domain boilerplate", line: "请记住本站域名，最新网址稍后公布。", wantIssue: true},
		{name: "source website", line: "本书来自某某小说网，欢迎收藏。", wantIssue: true},
		{name: "public account promotion", line: "关注公众号，回复小说领取全文。", wantIssue: true},
		{name: "story website wording", line: "他盯着网站报错页面，问这个网址为什么打不开。", wantIssue: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := parsePreflightForTest(t, []string{"第一章 开始", tt.line, "第二章 继续", "正文"})
			var found bool
			for _, issue := range report.Issues {
				if issue.Type == PreflightIssueThirdPartyAd {
					found = true
					if issue.Detector == "" || issue.LineNumber != 4 || issue.ChapterTitle != "第一章 开始" {
						t.Fatalf("third-party issue lacks expected evidence: %+v", issue)
					}
				}
			}
			if found != tt.wantIssue {
				t.Fatalf("third-party issue found = %v, want %v; issues: %+v", found, tt.wantIssue, report.Issues)
			}
		})
	}
}

type matchingThirdPartyDetector string

func (detector matchingThirdPartyDetector) Name() string {
	return string(detector)
}

func (matchingThirdPartyDetector) Detect(string) (string, bool) {
	return "matched", true
}

func TestDetectThirdPartyIssuesReportsOneIssuePerLine(t *testing.T) {
	report := newPreflightReport()
	detectThirdPartyIssues(report, []thirdPartyDetector{
		matchingThirdPartyDetector("first"),
		matchingThirdPartyDetector("second"),
	}, 8, "第一章", "example.com")
	if len(report.Issues) != 1 || report.Issues[0].Detector != "first" {
		t.Fatalf("issues = %+v, want first matching detector only", report.Issues)
	}
}

func TestEPUBConverterPreflightBlocksUnlessAllowed(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)
	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "story.txt")
	content := strings.Join([]string{
		"预检测试小说",
		"作者：张三",
		"第一章 开始",
		"正文",
		"第三章 跳章",
		"正文",
	}, "\n")
	if err := os.WriteFile(txtPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	blockedOutput := filepath.Join(tmpDir, "blocked.epub")
	blockedBook := &Book{Filename: txtPath, Output: blockedOutput}
	for attempt := 1; attempt <= 2; attempt++ {
		err := NewEPUBConverter().Convert(context.Background(), blockedBook)
		var preflightErr *PreflightError
		if !errors.As(err, &preflightErr) {
			t.Fatalf("Convert attempt %d error = %v, want *PreflightError", attempt, err)
		}
		if len(preflightErr.Report.Issues) != 1 || preflightErr.Report.Issues[0].Type != PreflightIssueMissingChapter {
			t.Fatalf("unexpected preflight report: %+v", preflightErr.Report)
		}
		if _, statErr := os.Stat(blockedOutput); !os.IsNotExist(statErr) {
			t.Fatalf("blocked conversion created output or returned unexpected stat error: %v", statErr)
		}
	}

	allowedOutput := filepath.Join(tmpDir, "allowed.epub")
	allowedBook := &Book{Filename: txtPath, Output: allowedOutput, AllowAnomalies: true}
	if err := NewEPUBConverter().Convert(context.Background(), allowedBook); err != nil {
		t.Fatalf("allowed conversion failed: %v", err)
	}
	if _, err := os.Stat(allowedOutput); err != nil {
		t.Fatalf("expected allowed EPUB output: %v", err)
	}
}

func parsePreflightForTest(t *testing.T, body []string) *PreflightReport {
	t.Helper()
	isolateAutoRuleConfigDiscovery(t)
	txtPath := filepath.Join(t.TempDir(), "story.txt")
	content := strings.Join(append([]string{"测试小说", "作者：测试作者"}, body...), "\n")
	if err := os.WriteFile(txtPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	book := &Book{Filename: txtPath}
	if err := book.FullDefault(); err != nil {
		t.Fatalf("book defaults: %v", err)
	}
	report, err := (&epubConverter{}).parse(context.Background(), book, book.parseRules)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return report
}
