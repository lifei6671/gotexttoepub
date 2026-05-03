package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestFormatCLIErrorRequiredFlag(t *testing.T) {
	message := FormatCLIError(cli.Exit(`Required flag "file" not set`, 1))
	if !strings.Contains(message, "缺少必填参数: --file") {
		t.Fatalf("expected chinese required flag message, got: %s", message)
	}
}

func TestFormatCLIErrorUnknownFlag(t *testing.T) {
	message := FormatCLIError(cli.Exit("flag provided but not defined: --bad-flag", 1))
	if !strings.Contains(message, "存在未知参数: --bad-flag") {
		t.Fatalf("expected chinese unknown flag message, got: %s", message)
	}
}

func TestHandleCommandNotFoundWritesChineseMessage(t *testing.T) {
	var buffer bytes.Buffer
	app := &cli.App{
		ErrWriter: &buffer,
	}

	HandleCommandNotFound(&cli.Context{App: app}, "unknown")
	output := buffer.String()
	if !strings.Contains(output, "未知命令: unknown") {
		t.Fatalf("expected chinese unknown command message, got: %s", output)
	}
}
