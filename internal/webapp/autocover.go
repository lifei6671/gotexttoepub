package webapp

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/lifei6671/gotexttoepub/goepub"
	"github.com/lifei6671/gotexttoepub/internal/jobs"
)

const autoCoverSVGTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="900" height="1200" viewBox="0 0 900 1200" role="img" aria-label="自动生成的小说封面">
  <rect width="900" height="1200" fill="#25221d"/>
  <rect width="900" height="260" fill="#3c352c"/>
  <rect y="940" width="900" height="260" fill="#562a24"/>
  <rect x="94" y="94" width="712" height="1012" fill="none" stroke="#f2ecdf" stroke-opacity=".42" stroke-width="5"/>
  <rect x="116" y="116" width="668" height="968" fill="none" stroke="#f2ecdf" stroke-opacity=".18" stroke-width="2"/>
  <path d="M136 877 A210 210 0 0 1 449 1050" fill="none" stroke="#e6c18e" stroke-opacity=".64" stroke-width="10"/>
  <path d="M546 820 A151 151 0 0 1 772 950" fill="none" stroke="#a63b2a" stroke-opacity=".9" stroke-width="10"/>
  <text x="450" y="395" fill="#f2ecdf" font-size="86" font-weight="700" text-anchor="middle" font-family="Songti SC, STSong, SimSun, serif">{{TITLE_LINE_1}}</text>
  <text x="450" y="520" fill="#f2ecdf" font-size="86" font-weight="700" text-anchor="middle" font-family="Songti SC, STSong, SimSun, serif">{{TITLE_LINE_2}}</text>
  <text x="450" y="652" fill="#f2ecdf" fill-opacity=".72" font-size="30" font-weight="600" text-anchor="middle" font-family="Songti SC, STSong, SimSun, serif">{{AUTHOR}}</text>
  <rect x="654" y="954" width="88" height="88" fill="#a63b2a"/>
  <rect x="665" y="965" width="66" height="66" fill="none" stroke="#f2ecdf" stroke-opacity=".88" stroke-width="2"/>
  <text x="698" y="1013" fill="#f2ecdf" font-size="40" font-weight="700" text-anchor="middle" font-family="Songti SC, STSong, SimSun, serif">著</text>
</svg>`

func newAutoCoverGenerator(workDir string) goepub.AutoCoverFunc {
	return func(ctx context.Context, title, author string) (string, func(), error) {
		if err := ctx.Err(); err != nil {
			return "", nil, fmt.Errorf("%w: %v", jobs.ErrCoverGeneration, err)
		}
		content, err := buildAutoCoverSVG(title, author)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", jobs.ErrCoverGeneration, err)
		}
		file, err := os.CreateTemp(workDir, "auto-cover-*.svg")
		if err != nil {
			return "", nil, fmt.Errorf("%w: 创建 SVG 文件失败: %v", jobs.ErrCoverGeneration, err)
		}
		path := file.Name()
		cleanup := func() { _ = os.Remove(path) }
		if _, err := file.WriteString(content); err != nil {
			_ = file.Close()
			cleanup()
			return "", nil, fmt.Errorf("%w: 写入 SVG 文件失败: %v", jobs.ErrCoverGeneration, err)
		}
		if err := file.Close(); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("%w: 关闭 SVG 文件失败: %v", jobs.ErrCoverGeneration, err)
		}
		return filepath.Clean(path), cleanup, nil
	}
}

func buildAutoCoverSVG(title, author string) (string, error) {
	titleLines := autoCoverTitleLines(title)
	escapedTitle1, err := escapeSVGText(titleLines[0])
	if err != nil {
		return "", err
	}
	escapedTitle2, err := escapeSVGText(titleLines[1])
	if err != nil {
		return "", err
	}
	escapedAuthor, err := escapeSVGText(autoCoverAuthor(author))
	if err != nil {
		return "", err
	}

	return strings.NewReplacer(
		"{{TITLE_LINE_1}}", escapedTitle1,
		"{{TITLE_LINE_2}}", escapedTitle2,
		"{{AUTHOR}}", escapedAuthor,
	).Replace(autoCoverSVGTemplate), nil
}

func autoCoverTitleLines(title string) [2]string {
	title = normalizeCoverText(title)
	if title == "" {
		title = "未命名文稿"
	}
	runes := []rune(truncateCoverText(title, 22))
	if len(runes) <= 11 {
		return [2]string{string(runes), ""}
	}
	return [2]string{string(runes[:11]), string(runes[11:])}
}

func autoCoverAuthor(author string) string {
	author = normalizeCoverText(author)
	if author == "" {
		return "作者：未署名"
	}
	return "作者：" + truncateCoverText(author, 28)
}

func normalizeCoverText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateCoverText(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes-1]) + "…"
}

func escapeSVGText(value string) (string, error) {
	var builder strings.Builder
	if err := xml.EscapeText(&builder, []byte(value)); err != nil {
		return "", fmt.Errorf("转义 SVG 文本失败: %w", err)
	}
	return builder.String(), nil
}
