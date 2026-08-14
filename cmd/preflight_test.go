package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/lifei6671/gotexttoepub/goepub"
)

func TestConfirmContinue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "yes short", input: "y\n", want: true},
		{name: "yes word", input: "YES\r\n", want: true},
		{name: "default no", input: "\n", want: false},
		{name: "explicit no", input: "n\n", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			got, err := confirmContinue(strings.NewReader(test.input), &output)
			if err != nil {
				t.Fatalf("confirmContinue() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("confirmContinue() = %v, want %v", got, test.want)
			}
			if !strings.Contains(output.String(), "[y/N]") {
				t.Fatalf("prompt = %q, want confirmation hint", output.String())
			}
		})
	}
}

func TestPrintPreflightReport(t *testing.T) {
	current := 13
	report := goepub.PreflightReport{
		Issues: []goepub.PreflightIssue{{
			Type:           "missing_chapter",
			Severity:       "warning",
			LineNumber:     12,
			ChapterTitle:   "第13章 继续",
			Message:        "疑似缺少第 12 章",
			Evidence:       "第11章 → 第13章",
			CurrentChapter: &current,
		}},
	}
	var output bytes.Buffer
	if err := printPreflightReport(&output, report); err != nil {
		t.Fatalf("printPreflightReport() error = %v", err)
	}

	for _, want := range []string{"1 项文稿异常", "第 12 行", "第13章 继续", "第11章 → 第13章"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("report output = %q, want %q", output.String(), want)
		}
	}
}

func TestEpubCommandAllowAnomaliesFlag(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "story.txt")
	content := "命令行预检测试\n作者：测试者\n第一章 开始\n正文\n第三章 跳章\n正文\n"
	if err := os.WriteFile(inputPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		allow      bool
		wantExit   int
		wantOutput bool
	}{
		{name: "non-interactive blocks", wantExit: 2},
		{name: "explicit allow converts", allow: true, wantOutput: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outputPath := filepath.Join(tmpDir, strings.ReplaceAll(test.name, " ", "-")+".epub")
			var output bytes.Buffer
			app := &cli.App{
				Commands:  []*cli.Command{newEpubCommand()},
				Reader:    strings.NewReader("y\n"),
				Writer:    &output,
				ErrWriter: &output,
			}
			args := []string{"gotexttoepub", "epub", "--file", inputPath, "--output", outputPath}
			if test.allow {
				args = append(args, "--allow-anomalies")
			}
			err := app.Run(args)
			if test.wantExit == 0 && err != nil {
				t.Fatalf("app.Run() error = %v", err)
			}
			if test.wantExit != 0 {
				var exitCoder cli.ExitCoder
				if !errors.As(err, &exitCoder) || exitCoder.ExitCode() != test.wantExit {
					t.Fatalf("app.Run() error = %v, want exit %d", err, test.wantExit)
				}
			}
			_, statErr := os.Stat(outputPath)
			if test.wantOutput && statErr != nil {
				t.Fatalf("expected EPUB output: %v", statErr)
			}
			if !test.wantOutput && !os.IsNotExist(statErr) {
				t.Fatalf("blocked conversion output stat error = %v, want not exist", statErr)
			}
		})
	}
}

func TestConvertWithPreflightConfirmationRejectsNonInteractiveInput(t *testing.T) {
	app := &cli.App{Reader: strings.NewReader("y\n"), ErrWriter: &bytes.Buffer{}}
	context := &cli.Context{App: app}
	err := convertWithPreflightConfirmation(context, &goepub.Book{Filename: "missing.txt"})
	if err == nil {
		t.Fatal("convertWithPreflightConfirmation() error = nil")
	}
	var exitCoder cli.ExitCoder
	if errors.As(err, &exitCoder) && exitCoder.ExitCode() == 2 {
		t.Fatal("missing file must remain a hard conversion error, not a preflight choice")
	}
}
