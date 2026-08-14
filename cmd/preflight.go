package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/lifei6671/gotexttoepub/goepub"
)

func convertWithPreflightConfirmation(c *cli.Context, book *goepub.Book) error {
	converter := goepub.NewEPUBConverter()
	err := converter.Convert(c.Context, book)
	var preflightErr *goepub.PreflightError
	if !errors.As(err, &preflightErr) {
		return err
	}

	writer := commandErrorWriter(c)
	if printErr := printPreflightReport(writer, preflightErr.Report); printErr != nil {
		return fmt.Errorf("输出异常检测报告失败: %w", printErr)
	}
	if !c.Bool("allow-anomalies") {
		reader := commandReader(c)
		if !isInteractiveReader(reader) {
			return cli.Exit("检测到文稿异常，非交互环境请修正文稿，或使用 --allow-anomalies 明确继续", 2)
		}
		confirmed, confirmErr := confirmContinue(reader, writer)
		if confirmErr != nil {
			return fmt.Errorf("读取确认选择失败: %w", confirmErr)
		}
		if !confirmed {
			return cli.Exit("已取消转换，请修正文稿后重试", 2)
		}
	}

	book.AllowAnomalies = true
	return converter.Convert(c.Context, book)
}

func printPreflightReport(writer io.Writer, report goepub.PreflightReport) error {
	if _, err := fmt.Fprintf(writer, "检测到 %d 项文稿异常，已暂停转换：\n", len(report.Issues)); err != nil {
		return err
	}
	for index, issue := range report.Issues {
		if _, err := fmt.Fprintf(writer, "\n%d. %s", index+1, issue.Message); err != nil {
			return err
		}
		if issue.LineNumber > 0 {
			if _, err := fmt.Fprintf(writer, "（第 %d 行）", issue.LineNumber); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
		if issue.ChapterTitle != "" {
			if _, err := fmt.Fprintf(writer, "   章节：%s\n", issue.ChapterTitle); err != nil {
				return err
			}
		}
		if issue.Evidence != "" {
			if _, err := fmt.Fprintf(writer, "   证据：%s\n", issue.Evidence); err != nil {
				return err
			}
		}
	}
	if report.Truncated {
		if _, err := fmt.Fprintln(writer, "\n异常数量较多，以上报告已截断。"); err != nil {
			return err
		}
	}
	return nil
}

func confirmContinue(reader io.Reader, writer io.Writer) (bool, error) {
	if _, err := fmt.Fprint(writer, "\n是否仍要继续转换？[y/N] "); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func commandReader(c *cli.Context) io.Reader {
	if c != nil && c.App != nil && c.App.Reader != nil {
		return c.App.Reader
	}
	return os.Stdin
}

func commandErrorWriter(c *cli.Context) io.Writer {
	if c != nil && c.App != nil && c.App.ErrWriter != nil {
		return c.App.ErrWriter
	}
	return os.Stderr
}

func isInteractiveReader(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
