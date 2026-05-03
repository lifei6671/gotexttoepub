package goepub

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	defaultLanguage     = "zh-CN"
	defaultEncoding     = "auto"
	defaultPresetMode   = presetModeSuggest
	maxScannerTokenSize = 4 * 1024 * 1024
)

// 文件解析规则的正则表达式
const (
	TitlePattern   = `^\S+.*$`
	AuthorPattern  = `^作者[:：](.*)$`
	IntroPattern   = `^(内容简介|简介|楔子|引子|序|序言)$`
	VolumePattern  = `^(第[一二三四五六七八九十百零0-9]+(卷|部|集))([\s　:：\-—].{0,30})?$`
	ChapterPattern = `^((第[一二三四五六七八九十百千万零0-9]+(章|回|节))|(完本感言)).{0,40}$`
	ExtraPattern   = `^番外.{0,30}$`
	ParagraphStart = "<p style=\"text-indent: 2em;\">"
	ParagraphEnd   = "</p>\n"
)

// Volume 表示一本书中的“卷”。
// 有些小说没有卷的概念，此时 Title 可以为空，只包含章节列表。
type Volume struct {
	Title    string
	Chapters []Chapter
}

// Chapter 表示单个章节。
// Content 使用 strings.Builder，避免正文拼接时产生大量中间字符串。
type Chapter struct {
	Title   string
	Content strings.Builder
}

// Book 描述一次转换任务所需的全部输入和解析配置。
// 它既可以由命令行参数组装，也可以由库调用方手动构造。
type Book struct {
	// Name 是书名；留空时会从 TXT 首个非空行中自动提取。
	Name string
	// Author 是作者名；留空时会尝试从“作者：xxx”形式的行中提取。
	Author string
	// Volumes 是解析后的卷章结构。
	Volumes []Volume
	// Cover 支持本地路径或网络 URL。
	Cover string
	// Lang 是 EPUB 语言标识，默认使用 zh-CN。
	Lang string
	// Encoding 是输入 TXT 的编码格式，默认 auto。
	// 当前支持 auto、utf-8、gbk、gb18030。
	Encoding string
	// Intro 是图书简介；留空时会尝试从“简介/序/楔子”等章节推导。
	Intro       string
	Publisher   string
	PublishDate string
	// Filename 是输入 TXT 文件路径。
	Filename string
	// Output 既可以是输出文件路径，也可以是输出目录。
	Output string
	// RulePresets 是可选的命名规则预设列表。
	// 预设用于在通用内置规则基础上，叠加少量站点或来源特征规则。
	RulePresets []string
	// RuleChannel 是规则配置中要使用的渠道名称。
	// 例如 qidian、fanqie；留空时使用规则文件中的默认渠道。
	RuleChannel string
	// RulePresetMode 控制自动探测预设时的行为。
	// 当前支持 off、suggest、apply，默认 suggest。
	RulePresetMode string
	// RuleConfigPath 是可选的规则配置文件路径。
	// 留空时完全使用内置规则，填写后会在内置规则基础上做覆盖。
	RuleConfigPath string

	// VolumeRegex 用于识别卷标题。
	VolumeRegex *regexp.Regexp
	// TitleRegex 用于识别书名。
	TitleRegex *regexp.Regexp
	// AuthorRegex 用于识别作者行。
	AuthorRegex *regexp.Regexp
	// ChapterRegex 用于识别章节标题。
	ChapterRegex *regexp.Regexp
	// ExtraRegex 用于识别“番外”等特殊章节。
	ExtraRegex *regexp.Regexp
	// IntroRegex 用于识别简介类章节。
	IntroRegex *regexp.Regexp

	parseRules          *ParseRules
	detectedRulePresets []string
}

// FullDefault 填充默认值并规范化路径。
func (book *Book) FullDefault() error {
	if strings.TrimSpace(book.Lang) == "" {
		book.Lang = defaultLanguage
	}
	if strings.TrimSpace(book.Encoding) == "" {
		book.Encoding = defaultEncoding
	}
	if strings.TrimSpace(book.RulePresetMode) == "" {
		book.RulePresetMode = defaultPresetMode
	}

	filename, err := expandPath(book.Filename)
	if err != nil {
		return fmt.Errorf("解析输入文件路径失败: %w", err)
	}
	if strings.TrimSpace(filename) == "" {
		return fmt.Errorf("TXT 文件路径不能为空")
	}
	book.Filename, err = filepath.Abs(filename)
	if err != nil {
		return fmt.Errorf("解析输入文件绝对路径失败: %w", err)
	}

	if strings.TrimSpace(book.Output) != "" {
		output, err := expandPath(book.Output)
		if err != nil {
			return fmt.Errorf("解析输出路径失败: %w", err)
		}
		book.Output = output
	}
	if strings.TrimSpace(book.RuleConfigPath) != "" {
		ruleConfigPath, err := expandPath(book.RuleConfigPath)
		if err != nil {
			return fmt.Errorf("解析规则配置路径失败: %w", err)
		}
		book.RuleConfigPath = ruleConfigPath
	}

	if strings.TrimSpace(book.Cover) != "" && !isURLorFTP(book.Cover) {
		cover, err := expandPath(book.Cover)
		if err != nil {
			return fmt.Errorf("解析封面路径失败: %w", err)
		}
		book.Cover = cover
	}

	book.Name = strings.TrimSpace(book.Name)
	book.Author = strings.TrimSpace(book.Author)
	book.Encoding = normalizeEncodingName(book.Encoding)
	book.Intro = strings.TrimSpace(book.Intro)
	book.RulePresetMode = normalizePresetMode(book.RulePresetMode)

	parseRules, err := buildParseRules(book)
	if err != nil {
		return err
	}
	book.parseRules = parseRules
	book.VolumeRegex = parseRules.VolumeRegex
	book.ChapterRegex = parseRules.ChapterRegex
	book.ExtraRegex = parseRules.ExtraRegex
	book.IntroRegex = parseRules.IntroRegex
	return nil
}

// OutputPath 根据书名、输入文件名和输出参数，推导最终的 EPUB 文件路径。
// 这样命令行可以同时支持“输出目录”和“输出文件”两种写法。
func (book *Book) OutputPath() (string, error) {
	filename := sanitizeFileName(book.Name)
	if filename == "" {
		filename = sanitizeFileName(strings.TrimSuffix(filepath.Base(book.Filename), filepath.Ext(book.Filename)))
	}
	if filename == "" {
		filename = "book"
	}

	if strings.TrimSpace(book.Output) == "" {
		return filepath.Abs(filename + ".epub")
	}

	output := book.Output
	stat, err := os.Stat(output)
	if err == nil && stat.IsDir() {
		return filepath.Join(output, filename+".epub"), nil
	}
	if err == nil && !stat.IsDir() {
		if strings.EqualFold(filepath.Ext(output), ".epub") {
			return output, nil
		}
	}

	if strings.EqualFold(filepath.Ext(output), ".epub") {
		return output, nil
	}
	return filepath.Join(output, filename+".epub"), nil
}

// FlagParse 是旧版 flag 风格的参数解析入口。
// 目前主要保留兼容性，新的命令行入口已切换到 urfave/cli。
func FlagParse() *Book {
	var book Book
	flag.StringVar(&book.Name, "name", "", "书名，不填写程序会自动解析")
	flag.StringVar(&book.Author, "author", "", "作者，不填写程序会自动解析")
	flag.StringVar(&book.Lang, "lang", "", "语言，默认中文，可设置其他语言如：en,de,fr,it,es,zh,ja,pt,ru,nl")
	flag.StringVar(&book.Encoding, "encoding", "", "文本编码，默认 auto，可设置 utf-8、gbk、gb18030")
	flag.StringVar(&book.Cover, "cover", "", "封面图片的路径，可以是本地文件路径也可以是网络图片url")
	flag.StringVar(&book.Intro, "intro", "", "简介，不填写程序会自动解析")
	flag.StringVar(&book.Publisher, "publisher", "", "出版社")
	flag.StringVar(&book.PublishDate, "date", "", "出版日期")
	flag.StringVar(&book.Filename, "file", "", "文件路径")
	flag.StringVar(&book.Output, "output", "", "输出路径")
	var rulePreset string
	flag.StringVar(&rulePreset, "rule-preset", "", "规则预设名称，多个预设使用逗号分隔")
	flag.StringVar(&book.RuleChannel, "rule-channel", "", "规则渠道名称，例如 default、qidian、fanqie")
	flag.StringVar(&book.RulePresetMode, "rule-preset-mode", "", "自动探测预设的行为模式：off、suggest、apply")
	flag.StringVar(&book.RuleConfigPath, "rule-config", "", "规则配置文件路径，默认使用内置规则，推荐使用带注释的 TOML 文件")
	var titlePattern string
	flag.StringVar(&titlePattern, "title-regexp", "", "书名的解析规则，不填写程序会自动解析")
	var authorPattern string
	flag.StringVar(&authorPattern, "author-regexp", "", "作者的解析规则，不填写程序会自动解析")
	var volumePattern string
	flag.StringVar(&volumePattern, "volume-pattern", "", "卷的解析规则,不填写程序会自动解析")
	var chapterPattern string
	flag.StringVar(&chapterPattern, "chapter-pattern", "", "章节的解析规则，不填写程序会自动解析")
	var extraPattern string
	flag.StringVar(&extraPattern, "extra-pattern", "", "番外的解析规则，不填写程序会自动解析")
	var introPattern string
	flag.StringVar(&introPattern, "intro-pattern", "", "简介的解析规则，不填写程序会自动解析")
	flag.Parse()

	if volumePattern != "" {
		book.VolumeRegex = regexp.MustCompile(volumePattern)
	}
	if chapterPattern != "" {
		book.ChapterRegex = regexp.MustCompile(chapterPattern)
	}
	if extraPattern != "" {
		book.ExtraRegex = regexp.MustCompile(extraPattern)
	}
	if introPattern != "" {
		book.IntroRegex = regexp.MustCompile(introPattern)
	}
	if titlePattern != "" {
		book.TitleRegex = regexp.MustCompile(titlePattern)
	}
	if authorPattern != "" {
		book.AuthorRegex = regexp.MustCompile(authorPattern)
	}
	book.RulePresets = normalizeRulePresetNames(rulePreset)
	return &book
}

// expandPath 将 ~ 开头的路径展开为用户主目录，兼容常见命令行写法。
func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// sanitizeFileName 清理文件名中的非法字符，避免在 Windows 等平台写文件失败。
func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer(
		"<", "_",
		">", "_",
		":", "_",
		"\"", "_",
		"/", "_",
		"\\", "_",
		"|", "_",
		"?", "_",
		"*", "_",
	)
	name = strings.TrimSpace(replacer.Replace(name))
	name = strings.Trim(name, ". ")
	return name
}
