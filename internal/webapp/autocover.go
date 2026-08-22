package webapp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/lifei6671/gotexttoepub/goepub"
	"github.com/lifei6671/gotexttoepub/internal/jobs"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	autoCoverWidth      = 900
	autoCoverHeight     = 1200
	autoCoverTitleWidth = 620
)

func newAutoCoverGenerator(workDir string) goepub.AutoCoverFunc {
	return func(ctx context.Context, title, author string) (string, func(), error) {
		if err := ctx.Err(); err != nil {
			return "", nil, fmt.Errorf("%w: %v", jobs.ErrCoverGeneration, err)
		}
		content, err := buildAutoCoverPNG(title, author)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", jobs.ErrCoverGeneration, err)
		}
		file, err := os.CreateTemp(workDir, "auto-cover-*.png")
		if err != nil {
			return "", nil, fmt.Errorf("%w: 创建 PNG 文件失败: %v", jobs.ErrCoverGeneration, err)
		}
		path := file.Name()
		cleanup := func() { _ = os.Remove(path) }
		if _, err := file.Write(content); err != nil {
			_ = file.Close()
			cleanup()
			return "", nil, fmt.Errorf("%w: 写入 PNG 文件失败: %v", jobs.ErrCoverGeneration, err)
		}
		if err := file.Close(); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("%w: 关闭 PNG 文件失败: %v", jobs.ErrCoverGeneration, err)
		}
		return filepath.Clean(path), cleanup, nil
	}
}

func buildAutoCoverPNG(title, author string) ([]byte, error) {
	fontData, err := goepub.DefaultCoverFont()
	if err != nil {
		return nil, err
	}
	coverFont, err := opentype.Parse(fontData)
	if err != nil {
		return nil, fmt.Errorf("解析内置封面字体失败: %w", err)
	}
	titleLines := autoCoverTitleLines(title)
	titleFace, err := opentype.NewFace(coverFont, &opentype.FaceOptions{Size: autoCoverTitleFontSize(titleLines), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("创建封面标题字体失败: %w", err)
	}
	defer titleFace.Close()
	authorFace, err := opentype.NewFace(coverFont, &opentype.FaceOptions{Size: 30, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("创建封面作者字体失败: %w", err)
	}
	defer authorFace.Close()
	sealFace, err := opentype.NewFace(coverFont, &opentype.FaceOptions{Size: 40, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("创建封面印章字体失败: %w", err)
	}
	defer sealFace.Close()

	cover := image.NewRGBA(image.Rect(0, 0, autoCoverWidth, autoCoverHeight))
	drawAutoCoverBackground(cover)
	drawCenteredText(cover, titleFace, titleLines[0], autoCoverWidth/2, 395, color.RGBA{242, 236, 223, 255})
	drawCenteredText(cover, titleFace, titleLines[1], autoCoverWidth/2, 520, color.RGBA{242, 236, 223, 255})
	drawCenteredText(cover, authorFace, autoCoverAuthor(author), autoCoverWidth/2, 652, color.RGBA{242, 236, 223, 184})
	drawCenteredText(cover, sealFace, "著", 698, 1013, color.RGBA{242, 236, 223, 255})

	var output bytes.Buffer
	if err := png.Encode(&output, cover); err != nil {
		return nil, fmt.Errorf("编码自动封面 PNG 失败: %w", err)
	}
	return output.Bytes(), nil
}

func drawAutoCoverBackground(cover *image.RGBA) {
	fillRect(cover, cover.Bounds(), color.RGBA{37, 34, 29, 255})
	fillRect(cover, image.Rect(0, 0, autoCoverWidth, 260), color.RGBA{60, 53, 44, 255})
	fillRect(cover, image.Rect(0, 940, autoCoverWidth, autoCoverHeight), color.RGBA{86, 42, 36, 255})
	drawRectOutline(cover, image.Rect(94, 94, 806, 1106), 5, color.RGBA{242, 236, 223, 107})
	drawRectOutline(cover, image.Rect(116, 116, 784, 1084), 2, color.RGBA{242, 236, 223, 46})
	drawArc(cover, 345, 1050, 210, -2.15, -0.25, 10, color.RGBA{230, 193, 142, 163})
	drawArc(cover, 621, 950, 151, -2.10, 0, 10, color.RGBA{166, 59, 42, 230})
	fillRect(cover, image.Rect(654, 954, 742, 1042), color.RGBA{166, 59, 42, 255})
	drawRectOutline(cover, image.Rect(665, 965, 731, 1031), 2, color.RGBA{242, 236, 223, 224})
}

func fillRect(dst draw.Image, rect image.Rectangle, source color.Color) {
	draw.Draw(dst, rect, image.NewUniform(source), image.Point{}, draw.Over)
}

func drawRectOutline(dst draw.Image, rect image.Rectangle, width int, source color.Color) {
	for offset := 0; offset < width; offset++ {
		line := rect.Inset(offset)
		fillRect(dst, image.Rect(line.Min.X, line.Min.Y, line.Max.X, line.Min.Y+1), source)
		fillRect(dst, image.Rect(line.Min.X, line.Max.Y-1, line.Max.X, line.Max.Y), source)
		fillRect(dst, image.Rect(line.Min.X, line.Min.Y, line.Min.X+1, line.Max.Y), source)
		fillRect(dst, image.Rect(line.Max.X-1, line.Min.Y, line.Max.X, line.Max.Y), source)
	}
}

func drawArc(dst *image.RGBA, centerX, centerY, radius int, start, end float64, width int, source color.Color) {
	for angle := start; angle <= end; angle += 0.004 {
		x := centerX + int(math.Round(float64(radius)*math.Cos(angle)))
		y := centerY + int(math.Round(float64(radius)*math.Sin(angle)))
		fillRect(dst, image.Rect(x-width/2, y-width/2, x+(width+1)/2, y+(width+1)/2), source)
	}
}

func drawCenteredText(dst draw.Image, face font.Face, value string, centerX, baseline int, source color.Color) {
	if value == "" {
		return
	}
	drawer := &font.Drawer{Dst: dst, Src: image.NewUniform(source), Face: face}
	width := drawer.MeasureString(value).Ceil()
	drawer.Dot = fixed.P(centerX-width/2, baseline)
	drawer.DrawString(value)
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

func autoCoverTitleFontSize(lines [2]string) float64 {
	lineLength := max(utf8.RuneCountInString(lines[0]), utf8.RuneCountInString(lines[1]))
	if lineLength == 0 {
		return 86
	}
	size := min(86.0, float64(autoCoverTitleWidth)/float64(lineLength))
	return max(52.0, size)
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
