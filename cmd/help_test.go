package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func newChineseHelpTestApp(buffer *bytes.Buffer) *cli.App {
	app := &cli.App{
		Name:     "gotexttoepub",
		Usage:    "将 TXT 小说转换为 EPUB 文件。",
		Version:  "1.2",
		Commands: []*cli.Command{newEpubCommand(), newRulesCommand()},
		Writer:   buffer,
	}
	ConfigureCLIHelp(app)
	return app
}

func TestAppHelpUsesChineseTemplate(t *testing.T) {
	var buffer bytes.Buffer
	app := newChineseHelpTestApp(&buffer)

	if err := app.Run([]string{"gotexttoepub", "--help"}); err != nil {
		t.Fatalf("run app help: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "名称:") {
		t.Fatalf("expected chinese help heading, got: %s", output)
	}
	if !strings.Contains(output, "用法:") {
		t.Fatalf("expected chinese usage heading, got: %s", output)
	}
	if !strings.Contains(output, "全局参数:") {
		t.Fatalf("expected chinese global flags heading, got: %s", output)
	}
	if !strings.Contains(output, "显示帮助") {
		t.Fatalf("expected chinese help flag usage, got: %s", output)
	}
	if strings.Contains(output, "GLOBAL OPTIONS:") {
		t.Fatalf("expected english headings to be removed, got: %s", output)
	}
}

func TestCommandHelpUsesChineseTemplate(t *testing.T) {
	var buffer bytes.Buffer
	app := newChineseHelpTestApp(&buffer)

	if err := app.Run([]string{"gotexttoepub", "epub", "--help"}); err != nil {
		t.Fatalf("run epub help: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "名称:") {
		t.Fatalf("expected chinese help heading, got: %s", output)
	}
	if !strings.Contains(output, "参数:") {
		t.Fatalf("expected chinese options heading, got: %s", output)
	}
	if strings.Contains(output, "OPTIONS:") {
		t.Fatalf("expected english options heading to be removed, got: %s", output)
	}
}
