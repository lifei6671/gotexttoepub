package goepub

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	goepub "github.com/go-shiori/go-epub"
	"github.com/vincent-petithory/dataurl"
)

//go:embed Fonts
var _fonts embed.FS

//go:embed Styles
var _styles embed.FS

type epub struct {
	txtp string
	*goepub.Epub
	content []*chapter
	author  string
	cover   string
	reg     *regexp.Regexp
	volume  *regexp.Regexp
}

type chapter struct {
	Title   string
	Content string
}

func NewConverter() *epub {
	return &epub{}
}

func (e *epub) SetContent(path string) *epub {
	e.txtp = path
	return e
}

func (e *epub) SetAuthor(author string) *epub {
	e.author = author
	return e
}

func (e *epub) SetCover(cover string) *epub {
	if strings.HasPrefix(cover, "http://") || strings.HasPrefix(cover, "https://") {
		resp, err := http.Get(cover)
		if err != nil {
			log.Printf("下载小说封面失败 -> %v", err)
			return e
		}
		defer resp.Body.Close()
		contentType := resp.Header.Get("Content-Type")
		var ext string
		exts, err := mime.ExtensionsByType(contentType)
		if err == nil && len(exts) > 0 {
			ext = exts[0]
		}
		if ext == "" {
			ext = ".jpg"
		}
		f, err := os.CreateTemp("", "cover*."+ext)
		if err != nil {
			log.Printf("生成临时文件失败 -> %v", err)
			return e
		}
		defer f.Close()
		_, err = io.Copy(f, resp.Body)
		if err != nil {
			log.Printf("保存封面失败 -> %v", err)
			return e
		}
		e.cover = f.Name()
	} else {
		e.cover = cover
	}
	return e
}

func (e *epub) SetRegExp(regexp *regexp.Regexp) *epub {
	e.reg = regexp
	return e
}

func (e *epub) SetVolumeReg(regexp *regexp.Regexp) *epub {
	e.volume = regexp
	return e
}

func (e *epub) run(style string) error {
	var err error

	// 变量定义
	var (
		title      string
		author     string
		currentVol *Volume
		currentCh  *Chapter
		volumes    []Volume
	)
	if e.txtp == "" {
		return errors.New("TXT 文件路径不能为空")
	}
	p, err := filepath.Abs(e.txtp)
	if err != nil {
		return err
	}
	e.txtp = p

	if e.reg == nil {
		return errors.New("章节提取正则错误")
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()
	// 行处理
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// 判断标题
		if title == "" && regexp.MustCompile(TitlePattern).MatchString(line) {
			title = line
			e.SetTitle(title)
			log.Printf("小说标题: %s", title)
			continue
		}

		// 判断作者
		if author == "" && regexp.MustCompile(AuthorPattern).MatchString(line) {
			matches := regexp.MustCompile(AuthorPattern).FindStringSubmatch(line)
			if len(matches) > 1 {
				author = strings.TrimSpace(matches[1])
				e.SetAuthor(author)
				e.author = author
				log.Printf("小说作者: %s", author)
			}
			continue
		}

		// 判断卷标题
		if e.volume != nil && e.volume.MatchString(line) {
			if currentCh != nil && currentVol != nil {
				currentVol.Chapters = append(currentVol.Chapters, *currentCh)
				currentCh = nil
			}
			// 处理当前卷
			if currentVol != nil {
				volumes = append(volumes, *currentVol)
			}
			log.Printf("解析卷: %s", line)
			currentVol = &Volume{Title: line}
			continue
		}
		// 判断章节
		if e.reg != nil && e.reg.MatchString(line) {
			if currentVol == nil {
				currentVol = &Volume{}
			}
			// 处理当前章节
			if currentCh != nil && currentVol != nil {
				currentVol.Chapters = append(currentVol.Chapters, *currentCh)
			}
			log.Printf("解析章节: %s", line)
			currentCh = &Chapter{Title: line}
			continue
		}
		// 排除一些特殊章节
		if line == "楔子" || line == "卷首语" || line == "序" || line == "楔子语" || strings.HasPrefix(line, "简介") || strings.HasPrefix(line, "内容简介") {
			if currentVol == nil {
				currentVol = &Volume{}
			}
			if currentCh != nil {
				currentVol.Chapters = append(currentVol.Chapters, *currentCh)
			}
			log.Printf("解析特殊章节: %s", line)
			currentCh = &Chapter{Title: line}
			continue
		}

		if regexp.MustCompile(ExtraPattern).MatchString(line) {
			// 将番外视为一个独立章节
			if currentCh != nil && currentVol != nil {
				currentVol.Chapters = append(currentVol.Chapters, *currentCh)
				currentCh = nil
			}
			if currentVol == nil {
				currentVol = &Volume{Title: "番外"}
			}
			currentCh = &Chapter{Title: line}
			continue
		}
		if currentCh != nil {
			// 章节内容
			lineText := strings.ReplaceAll(strings.ReplaceAll(line, "<", "&lt;"), ">", "&gt;")
			currentCh.Content.WriteString("<p style=\"text-indent:2em\">" + lineText + "</p>\n")
		}

	}

	// 处理最后一个卷和章节
	if currentCh != nil && currentVol != nil {
		currentVol.Chapters = append(currentVol.Chapters, *currentCh)
	}
	if currentVol != nil {
		volumes = append(volumes, *currentVol)
	}
	if len(volumes) > 0 {
		// 按照卷和章生成 EPUB
		for i, vol := range volumes {
			parentFilename := ""
			if vol.Title != "" {
				internalFilename := fmt.Sprintf("volume%d.xhtml", i)
				parentFilename, err = e.AddSection(fmt.Sprintf("<h1>%s</h1>", vol.Title), vol.Title, internalFilename, style)
				if err != nil {
					log.Printf("添加卷失败 -> %v", err)
					return err
				}
			}
			for j, ch := range vol.Chapters {
				// 如果第一个卷的第一个章节不是标题，则设置小说简介
				if i == 0 && j == 0 && vol.Title == "" && e.reg != nil && !e.reg.MatchString(ch.Title) {
					e.SetDescription(removeHTMLTags(ch.Content.String()))
				}
				chapterFilename := fmt.Sprintf("volume%d_chapter%d.xhtml", i, j)
				_, err = e.AddSubSection(parentFilename, fmt.Sprintf("<h2>%s</h2>%s", ch.Title, ch.Content.String()), ch.Title, chapterFilename, style)
				if err != nil {
					log.Printf("添加章节失败 ->卷：%s - 章: %s - 错误: %v", vol.Title, ch.Title, err)
					return err
				}
			}
		}
	}

	return nil
}

func (e *epub) Convert(save string) error {
	var err error
	// 创建 EPUB
	e.Epub, err = goepub.NewEpub("")
	if err != nil {
		return err
	}
	fErr := e.addFont()
	if fErr != nil {
		log.Printf("添加字体失败 -> %v", fErr)
		return fErr
	}
	style, cErr := e.addCSS()
	if cErr != nil {
		log.Printf("添加样式失败 -> %v", cErr)
		return cErr
	}
	if err := e.run(style); err != nil {
		return err
	}
	e.SetLang("zh-CN")

	e.Epub.SetAuthor(e.author)
	if e.cover != "" {
		cover, err := e.AddImage(e.cover, filepath.Base(e.cover))
		if err != nil {
			log.Printf("【%s】添加图片失败 -> %s %s", e.Title(), e.cover, err)
			return err
		} else {
			err := e.Epub.SetCover(cover, "")
			if err != nil {
				log.Printf("设置封面失败 -> %s", err)
				return err
			}
		}
	}
	if save == "" {
		s, err := filepath.Abs("./" + e.Title() + ".epub")
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Dir(s)); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(s), 0755); err != nil {
				log.Printf("创建目录失败 -> %s", err)
			}
		}
		save = s
	}

	return e.Write(save)
}

func (e *epub) addFont() error {
	err := fs.WalkDir(_fonts, "Fonts", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		log.Printf("添加字体: %s", path)
		data, err := _fonts.ReadFile(path)
		if err != nil {
			log.Printf("添加字代失败 -> %s %v", path, err)
			return err
		}
		dURL := dataurl.New(data, e.getMimeType(path))
		dURL.Encoding = dataurl.EncodingASCII
		_, err = e.AddFont(dURL.String(), filepath.Base(path))
		if err != nil {
			log.Printf("添加字体失败 -> %s %v", path, err)
			return fmt.Errorf("添加字体失败 ->%s %w", path, err)
		}
		return nil
	})
	return err
}

func (e *epub) addCSS() (string, error) {
	var styles []string
	// 遍历 Styles 目录下的所有文件
	err := fs.WalkDir(_styles, "Styles", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // 如果有错误直接返回
		}
		if !d.IsDir() { // 只处理文件
			log.Printf("添加样式: %s", path)
			// 读取文件内容
			data, err := _styles.ReadFile(path)
			if err != nil {
				log.Printf("Error reading file %s: %v\n", path, err)
				return nil // 继续处理其他文件
			}

			// 推断 MIME 类型
			mimeType := e.getMimeType(path)

			// 创建 dataurl
			dURL := dataurl.New(data, mimeType)
			dURL.Encoding = dataurl.EncodingASCII
			style, err := e.AddCSS(dURL.String(), filepath.Base(path))
			if err != nil {
				log.Printf("添加样式失败 -> %s %v", path, err)
				return err
			}
			styles = append(styles, style)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(styles) > 0 {
		cssContent := ""
		for _, style := range styles {
			cssContent += fmt.Sprintf("@import url('%s');\n", style)
		}
		dURL := dataurl.New([]byte(cssContent), "text/css")
		dURL.Encoding = dataurl.EncodingASCII
		css, err := e.AddCSS(dURL.String(), "styles.css")
		if err != nil {
			log.Printf("添加样式失败 -> %v", err)
			return "", err
		}
		return css, nil
	}
	return "", nil
}

// getMimeType 根据文件扩展名推断 MIME 类型
func (e *epub) getMimeType(path string) string {
	ext := strings.ToLower(path[strings.LastIndex(path, ".")+1:])
	switch ext {
	case "css":
		return "text/css"
	default:
		return "application/octet-stream" // 默认二进制流
	}
}

func removeHTMLTags(input string) string {
	// 定义正则表达式，用于匹配 HTML 标签
	re := regexp.MustCompile(`<[^>]*>`)
	// 替换所有匹配项为空字符串
	return re.ReplaceAllString(input, "")
}
