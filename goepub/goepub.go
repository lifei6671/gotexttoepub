package goepub

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	epublib "github.com/go-shiori/go-epub"
)

//go:embed Fonts
var embeddedFonts embed.FS

//go:embed Styles
var embeddedStyles embed.FS

type epub struct {
	book Book
}

// NewConverter 创建旧版链式调用风格的转换器。
// 它内部最终仍会走新的 Book + Converter 主流程，主要用于兼容历史用法。
func NewConverter() *epub {
	return &epub{}
}

// SetContent 设置待转换的 TXT 文件路径。
func (e *epub) SetContent(path string) *epub {
	e.book.Filename = path
	return e
}

// SetAuthor 手动指定作者，优先级高于自动解析。
func (e *epub) SetAuthor(author string) *epub {
	e.book.Author = author
	return e
}

// SetCover 设置封面路径或封面 URL。
func (e *epub) SetCover(cover string) *epub {
	e.book.Cover = cover
	return e
}

// SetRegExp 设置章节匹配正则。
func (e *epub) SetRegExp(regex *regexp.Regexp) *epub {
	e.book.ChapterRegex = regex
	return e
}

// SetVolumeReg 设置卷匹配正则。
func (e *epub) SetVolumeReg(regex *regexp.Regexp) *epub {
	e.book.VolumeRegex = regex
	return e
}

// Convert 兼容旧版 API，将链式配置转换为 Book 后交给统一转换器执行。
func (e *epub) Convert(save string) error {
	book := e.book
	book.Output = save
	return NewEPUBConverter().Convert(context.Background(), &book)
}

// addEmbeddedFonts 将内置字体复制为临时文件并注册到 EPUB。
// 之所以不用 data URL，是因为底层库对字体资源的兼容性更偏向文件输入。
func addEmbeddedFonts(e *epublib.Epub) (func(), error) {
	var cleanups []func()

	err := fs.WalkDir(embeddedFonts, "Fonts", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		log.Printf("添加字体: %s", path)
		data, err := embeddedFonts.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取字体失败 %s: %w", path, err)
		}

		source, cleanup, err := writeTempAsset(path, data)
		if err != nil {
			return err
		}

		if _, err := e.AddFont(source, filepath.Base(path)); err != nil {
			if cleanup != nil {
				cleanup()
			}
			return fmt.Errorf("添加字体失败 %s: %w", path, err)
		}
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}
		return nil
	})
	if err != nil {
		runCleanups(cleanups)
		return nil, err
	}
	return func() {
		runCleanups(cleanups)
	}, nil
}

// addEmbeddedStyles 将内置样式注册到 EPUB，并额外生成一个聚合样式文件统一导入。
func addEmbeddedStyles(e *epublib.Epub) (string, func(), error) {
	var imports []string
	var cleanups []func()

	err := fs.WalkDir(embeddedStyles, "Styles", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		log.Printf("添加样式: %s", path)
		data, err := embeddedStyles.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取样式失败 %s: %w", path, err)
		}

		source, cleanup, err := writeTempAsset(path, data)
		if err != nil {
			return err
		}

		style, err := e.AddCSS(source, filepath.Base(path))
		if err != nil {
			if cleanup != nil {
				cleanup()
			}
			return fmt.Errorf("添加样式失败 %s: %w", path, err)
		}
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}
		imports = append(imports, fmt.Sprintf("@import url('%s');", style))
		return nil
	})
	if err != nil {
		runCleanups(cleanups)
		return "", nil, err
	}

	if len(imports) == 0 {
		return "", func() {
			runCleanups(cleanups)
		}, nil
	}

	source, cleanup, err := writeTempAsset("styles.css", []byte(strings.Join(imports, "\n")))
	if err != nil {
		runCleanups(cleanups)
		return "", nil, err
	}
	if cleanup != nil {
		cleanups = append(cleanups, cleanup)
	}

	style, err := e.AddCSS(source, "styles.css")
	if err != nil {
		runCleanups(cleanups)
		return "", nil, fmt.Errorf("创建聚合样式失败: %w", err)
	}
	return style, func() {
		runCleanups(cleanups)
	}, nil
}

// mimeTypeByPath 按扩展名推断静态资源的 MIME 类型。
func mimeTypeByPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".css":
		return "text/css"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	default:
		if detected := mime.TypeByExtension(filepath.Ext(path)); detected != "" {
			return detected
		}
		return "application/octet-stream"
	}
}

// removeHTMLTags 用于从章节 XHTML 中提取纯文本简介。
func removeHTMLTags(input string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(input, "")
}

// writeTempAsset 将内置资源落盘到临时文件，并返回对应的清理函数。
// go-epub 在最终 Write 阶段才会真正读取这些资源，因此临时文件必须延迟清理。
func writeTempAsset(name string, data []byte) (string, func(), error) {
	ext := filepath.Ext(name)
	if ext == "" {
		ext = ".tmp"
	}

	f, err := os.CreateTemp("", "gotexttoepub-asset-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("创建临时资源文件失败 %s: %w", name, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("写入临时资源文件失败 %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("关闭临时资源文件失败 %s: %w", name, err)
	}

	return f.Name(), func() {
		_ = os.Remove(f.Name())
	}, nil
}

// runCleanups 按顺序执行资源清理函数。
func runCleanups(cleanups []func()) {
	for _, cleanup := range cleanups {
		if cleanup != nil {
			cleanup()
		}
	}
}
