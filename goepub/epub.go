package goepub

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	epublib "github.com/go-shiori/go-epub"
)

// epubConverter 是统一转换流程的 EPUB 实现。
// 它负责串联“文本解析、元信息设置、资源注入、文件输出”四个阶段。
type epubConverter struct{}

// Convert 执行完整的 TXT -> EPUB 转换流程。
func (c *epubConverter) Convert(ctx context.Context, book *Book) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if book == nil {
		return errors.New("book 不能为空")
	}
	if err := book.FullDefault(); err != nil {
		return err
	}
	rules := book.parseRules
	if len(book.Volumes) == 0 {
		// 如果调用方没有预先提供卷章结构，就从 TXT 原文中实时解析。
		if err := c.parse(ctx, book, rules); err != nil {
			return err
		}
	}

	e, err := epublib.NewEpub(book.Name)
	if err != nil {
		return fmt.Errorf("创建 EPUB 失败: %w", err)
	}

	if err := c.applyMetadata(book, e); err != nil {
		return err
	}
	fontCleanup, err := addEmbeddedFonts(e)
	if err != nil {
		return err
	}
	if fontCleanup != nil {
		defer fontCleanup()
	}

	style, styleCleanup, err := addEmbeddedStyles(e)
	if err != nil {
		return err
	}
	if styleCleanup != nil {
		defer styleCleanup()
	}
	if err := c.writeChapters(ctx, book, e, style); err != nil {
		return err
	}
	coverCleanup, err := c.setCover(ctx, book, e)
	if err != nil {
		return err
	}
	if coverCleanup != nil {
		defer coverCleanup()
	}

	output, err := book.OutputPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	return e.Write(output)
}

// WriteTo 将已经解析完成的卷章结构写入现有的 EPUB 对象。
// 这个方法主要为后续扩展或测试场景保留。
func (c *epubConverter) WriteTo(book *Book, e *epublib.Epub) error {
	return c.writeChapters(context.Background(), book, e, "")
}

// parse 负责把 TXT 文本解析成 Book.Volumes 结构。
// 它只关心“识别标题、作者、卷、章和正文”，不直接处理 EPUB 输出。
func (c *epubConverter) parse(ctx context.Context, book *Book, rules *ParseRules) error {
	raw, err := os.ReadFile(book.Filename)
	if err != nil {
		return fmt.Errorf("读取文件失败: %s - %w", book.Filename, err)
	}

	text, detectedEncoding, err := decodeTextContent(raw, book.Encoding)
	if err != nil {
		return err
	}
	log.Printf("检测到文本编码: %s", detectedEncoding)

	if len(book.RulePresets) == 0 && book.RulePresetMode != presetModeOff {
		detections := DetectRulePresets(text)
		if len(detections) > 0 {
			names := make([]string, 0, len(detections))
			reasonParts := make([]string, 0, len(detections))
			for _, detected := range detections {
				names = append(names, detected.Name)
				reasonParts = append(reasonParts, fmt.Sprintf("%s(score=%d, hit=%s)", detected.Name, detected.Score, strings.Join(detected.Reasons, " | ")))
			}

			switch book.RulePresetMode {
			case presetModeApply:
				book.detectedRulePresets = names
				rules, err = buildParseRules(book)
				if err != nil {
					return err
				}
				book.parseRules = rules
				book.VolumeRegex = rules.VolumeRegex
				book.ChapterRegex = rules.ChapterRegex
				book.ExtraRegex = rules.ExtraRegex
				book.IntroRegex = rules.IntroRegex
				log.Printf("自动应用规则预设: %s", strings.Join(reasonParts, "; "))
			default:
				log.Printf("检测到推荐规则预设: %s", strings.Join(reasonParts, "; "))
				log.Printf("如需自动应用，可使用 -rule-preset-mode=apply；如需手动指定，可使用 -rule-preset=%s", strings.Join(names, ","))
			}
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(text))
	// 默认 Scanner 单行长度限制较小，小说正文里常见的超长段落会直接触发错误，
	// 这里主动放大缓冲区以提升兼容性。
	scanner.Buffer(make([]byte, 64*1024), maxScannerTokenSize)

	var (
		currentVol      *Volume
		currentCh       *Chapter
		introLines      []string
		collectingIntro bool
	)

	flushChapter := func() {
		if currentVol == nil || currentCh == nil {
			return
		}
		currentVol.Chapters = append(currentVol.Chapters, *currentCh)
		currentCh = nil
	}
	flushVolume := func() {
		if currentVol == nil {
			return
		}
		if len(currentVol.Chapters) == 0 && strings.TrimSpace(currentVol.Title) == "" {
			return
		}
		book.Volumes = append(book.Volumes, *currentVol)
		currentVol = nil
	}

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if collectingIntro {
				continue
			}
			continue
		}

		if rules.ShouldIgnoreLine(line) {
			continue
		}

		if book.Name == "" || book.Author == "" {
			title, author, ok := rules.ParseInlineTitleAndAuthor(line)
			if ok {
				if book.Name == "" {
					book.Name = title
					log.Printf("小说标题: %s", book.Name)
				}
				if book.Author == "" {
					book.Author = author
					log.Printf("小说作者: %s", book.Author)
				}
				continue
			}
		}

		if book.Name == "" {
			if title, ok := rules.ParseTitle(line); ok {
				book.Name = title
				log.Printf("小说标题: %s", book.Name)
				continue
			}
		}

		if book.Author == "" {
			if author, ok := rules.ParseAuthor(line); ok {
				book.Author = author
				log.Printf("小说作者: %s", book.Author)
				continue
			}
		}

		if book.Intro == "" {
			if introText, ok := rules.ParsePrefixedIntro(line); ok {
				collectingIntro = true
				if introText != "" {
					introLines = append(introLines, introText)
				}
				continue
			}
			if collectingIntro {
				if rules.IsStructuralLine(line) {
					book.Intro = strings.TrimSpace(strings.Join(introLines, "\n"))
					collectingIntro = false
				} else {
					introLines = append(introLines, line)
					continue
				}
			}
		}

		switch {
		case rules.VolumeRegex != nil && rules.VolumeRegex.MatchString(line):
			// 遇到新卷时，先收束当前章节和当前卷，再开启下一卷。
			flushChapter()
			flushVolume()
			currentVol = &Volume{Title: line}
			log.Printf("解析卷: %s", line)
			continue
		case rules.ChapterRegex != nil && rules.ChapterRegex.MatchString(line):
			if currentVol == nil {
				// 无卷小说也允许直接挂章节，因此这里自动创建匿名卷。
				currentVol = &Volume{}
			}
			flushChapter()
			currentCh = &Chapter{Title: line}
			log.Printf("解析章节: %s", line)
			continue
		case rules.ExtraRegex != nil && rules.ExtraRegex.MatchString(line):
			if currentVol == nil {
				currentVol = &Volume{}
			}
			flushChapter()
			currentCh = &Chapter{Title: line}
			log.Printf("解析番外: %s", line)
			continue
		case rules.IsSpecialChapterTitle(line):
			if currentVol == nil {
				currentVol = &Volume{}
			}
			flushChapter()
			currentCh = &Chapter{Title: line}
			log.Printf("解析特殊章节: %s", line)
			continue
		}

		if currentCh != nil {
			// 普通正文仅归属到当前章节，且在写入前做 HTML 转义。
			currentCh.Content.WriteString(formatParagraph(line))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("扫描 TXT 文件失败: %w", err)
	}

	flushChapter()
	flushVolume()

	if book.Intro == "" && len(introLines) > 0 {
		book.Intro = strings.TrimSpace(strings.Join(introLines, "\n"))
	}
	if book.Name == "" {
		book.Name = strings.TrimSuffix(filepath.Base(book.Filename), filepath.Ext(book.Filename))
	}
	if book.Intro == "" {
		book.Intro = deriveIntro(book)
	}
	if len(book.Volumes) == 0 {
		return errors.New("未解析到任何章节，请检查章节正则是否正确")
	}
	return nil
}

// applyMetadata 将 Book 中的元信息同步到 EPUB 对象。
func (c *epubConverter) applyMetadata(book *Book, e *epublib.Epub) error {
	e.SetTitle(book.Name)
	e.SetLang(book.Lang)
	if book.Author != "" {
		e.SetAuthor(book.Author)
	}
	if book.Intro != "" {
		e.SetDescription(book.Intro)
	}
	return nil
}

// writeChapters 将卷章树写入 EPUB 文档结构。
// 卷会生成父级 section，章节会作为 subsection 挂载在卷下。
func (c *epubConverter) writeChapters(ctx context.Context, book *Book, e *epublib.Epub, style string) error {
	if len(book.Volumes) == 0 {
		return errors.New("卷不能为空")
	}

	for i, vol := range book.Volumes {
		if err := ctx.Err(); err != nil {
			return err
		}

		parentFilename := ""
		if vol.Title != "" {
			internalFilename := fmt.Sprintf("volume%d.xhtml", i)
			var err error
			parentFilename, err = e.AddSection(fmt.Sprintf("<h1>%s</h1>", html.EscapeString(vol.Title)), vol.Title, internalFilename, style)
			if err != nil {
				return fmt.Errorf("添加卷失败 %s: %w", vol.Title, err)
			}
		}

		for j, ch := range vol.Chapters {
			if err := ctx.Err(); err != nil {
				return err
			}

			chapterFilename := fmt.Sprintf("volume%d_chapter%d.xhtml", i, j)
			body := fmt.Sprintf("<h2>%s</h2>%s", html.EscapeString(ch.Title), ch.Content.String())
			if parentFilename == "" {
				if _, err := e.AddSection(body, ch.Title, chapterFilename, style); err != nil {
					return fmt.Errorf("添加章节失败 卷:%s 章:%s: %w", vol.Title, ch.Title, err)
				}
				continue
			}
			if _, err := e.AddSubSection(parentFilename, body, ch.Title, chapterFilename, style); err != nil {
				return fmt.Errorf("添加章节失败 卷:%s 章:%s: %w", vol.Title, ch.Title, err)
			}
		}
	}
	return nil
}

// setCover 将封面注入 EPUB。
// 封面既支持本地文件，也支持先下载到临时文件后再写入。
func (c *epubConverter) setCover(ctx context.Context, book *Book, e *epublib.Epub) (func(), error) {
	if strings.TrimSpace(book.Cover) == "" {
		return nil, nil
	}

	coverPath, cleanup, err := prepareCover(ctx, book.Cover)
	if err != nil {
		return nil, err
	}

	internalPath, err := e.AddImage(coverPath, filepath.Base(coverPath))
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("添加封面失败: %w", err)
	}
	if err := e.SetCover(internalPath, ""); err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("设置封面失败: %w", err)
	}
	return cleanup, nil
}

// NewEPUBConverter 创建一个新的 EPUB 转换器实现。
func NewEPUBConverter() Converter {
	return &epubConverter{}
}

// isURLorFTP 判断封面参数是否为可下载的远程地址。
func isURLorFTP(input string) bool {
	parsedURL, err := url.Parse(input)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false
	}
	switch strings.ToLower(parsedURL.Scheme) {
	case "http", "https", "ftp":
		return true
	default:
		return false
	}
}

// prepareCover 将封面转换为一个可被 go-epub 读取的本地文件路径。
// 如果传入的是远程地址，会先下载到临时文件并返回对应清理函数。
func prepareCover(ctx context.Context, cover string) (string, func(), error) {
	if !isURLorFTP(cover) {
		return cover, nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cover, nil)
	if err != nil {
		return "", nil, fmt.Errorf("创建封面请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "gotexttoepub/1.2")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("下载封面失败: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", nil, fmt.Errorf("下载封面失败: %s", resp.Status)
	}

	// 优先沿用远程 URL 的扩展名，这样后续媒体类型识别更稳定。
	ext := filepath.Ext(resp.Request.URL.Path)
	if ext == "" {
		ext = filepath.Ext(cover)
	}
	if ext == "" {
		ext = ".jpg"
	}

	f, err := os.CreateTemp("", "gotexttoepub-cover-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("创建封面临时文件失败: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("写入封面临时文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("关闭封面临时文件失败: %w", err)
	}

	return f.Name(), func() {
		_ = os.Remove(f.Name())
	}, nil
}

// formatParagraph 将原始文本行包装成 XHTML 段落。
func formatParagraph(line string) string {
	return ParagraphStart + html.EscapeString(line) + ParagraphEnd
}

// deriveIntro 从已解析的章节中提取简介内容。
// 仅在调用方未显式提供简介时使用。
func deriveIntro(book *Book) string {
	stripFirstParagraphTag := regexp.MustCompile(`(?i)^<p[^>]*>|</p>$`)
	for _, volume := range book.Volumes {
		for _, chapter := range volume.Chapters {
			if book.IntroRegex != nil && book.IntroRegex.MatchString(chapter.Title) {
				return strings.TrimSpace(removeHTMLTags(stripFirstParagraphTag.ReplaceAllString(chapter.Content.String(), "")))
			}
		}
	}
	return ""
}
