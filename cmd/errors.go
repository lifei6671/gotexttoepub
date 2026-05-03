package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/urfave/cli/v2"
)

var requiredFlagPattern = regexp.MustCompile(`Required flag "([^"]+)" not set`)
var unknownFlagPattern = regexp.MustCompile(`flag provided but not defined: -?(.+)`)

// FormatCLIError 将常见命令行错误转换成更友好的中文提示。
func FormatCLIError(err error) string {
	if err == nil {
		return ""
	}

	message := err.Error()
	if matches := requiredFlagPattern.FindStringSubmatch(message); len(matches) == 2 {
		return fmt.Sprintf("缺少必填参数: --%s\n可使用 \"gotexttoepub epub --help\" 查看完整参数说明。", matches[1])
	}
	if matches := unknownFlagPattern.FindStringSubmatch(message); len(matches) == 2 {
		flagName := strings.TrimLeft(strings.TrimSpace(matches[1]), "-")
		return fmt.Sprintf("存在未知参数: --%s\n可使用 \"gotexttoepub epub --help\" 查看支持的参数。", flagName)
	}
	if strings.Contains(message, "No help topic for") {
		return message
	}
	return message
}

// CLIExitCode 提取更合适的退出码。
func CLIExitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitCoder cli.ExitCoder
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return 1
}

// HandleCommandNotFound 统一未知命令提示，避免落回英文默认输出。
func HandleCommandNotFound(c *cli.Context, command string) {
	writer := io.Writer(os.Stderr)
	if c != nil && c.App != nil && c.App.ErrWriter != nil {
		writer = c.App.ErrWriter
	}

	fmt.Fprintf(writer, "未知命令: %s\n", strings.TrimSpace(command))
	fmt.Fprintln(writer, "可使用 \"gotexttoepub --help\" 查看可用命令。")
}
