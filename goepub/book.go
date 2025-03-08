package goepub

import (
	"flag"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// 文件解析规则的正则表达式
var (
	TitlePattern   = `^\S+.*$`                                          // 第一个非空白行是标题
	AuthorPattern  = `^作者[:：](.*)$`                                     // 第二个非空白行或以“作者”开头的是作者
	IntroPattern   = `^(内容简介|简介|楔子|引子|序|序言)$`                           // 内容简介/简介/楔子
	VolumePattern  = `^(第[一二三四五六七八九十百零0-9]+(卷|部|集)).{0,30}$`            // 卷标题
	ChapterPattern = `^((第[一二三四五六七八九十百千万零0-9]+(章|回|节))|(完本感言)).{0,40}$` // 章节标题
	ExtraPattern   = `^番外.{0,30}$`                                      // 番外部分
	ParagraphStart = "<p style=\"text-indent: 2em;\">"                  // 段落开始
	ParagraphEnd   = "</p>"
)

// Volume 卷的结构
type Volume struct {
	Title    string    // 卷标题
	Chapters []Chapter // 章节列表
}

// Chapter 章节的结构
type Chapter struct {
	Title   string          // 章节标题
	Content strings.Builder // 章节内容
}

type Book struct {
	// 标题
	Name string
	// 作者
	Author string
	// 解析后的章节
	Volumes []Volume
	// 封面
	Cover string
	// 语言
	Lang string
	// 简介
	Intro string
	// 出版社
	Publisher string
	// 出版日期
	PublishDate string
	// 文件路径
	Filename string
	// 输出路径
	Output string
	// 卷的解析规则
	VolumeRegex *regexp.Regexp
	// 章节的解析规则
	ChapterRegex *regexp.Regexp
	// 番外的解析规则
	ExtraRegex *regexp.Regexp
	// 简介的解析规则
	IntroRegex *regexp.Regexp
}

// FullDefault 填充默认值
func (book *Book) FullDefault() error {
	if book.VolumeRegex == nil {
		book.VolumeRegex = regexp.MustCompile(VolumePattern)
	}
	if book.ChapterRegex == nil {
		book.ChapterRegex = regexp.MustCompile(ChapterPattern)
	}
	if book.ExtraRegex == nil {
		book.ExtraRegex = regexp.MustCompile(ExtraPattern)
	}
	if book.IntroRegex == nil {
		book.IntroRegex = regexp.MustCompile(IntroPattern)
	}
	if book.Lang == "" {
		book.Lang = "zh"
	}

	if book.Filename == "" {
		return fmt.Errorf("TXT 文件路径不能为空: %s", book.Filename)
	}
	p, err := filepath.Abs(book.Filename)
	if err != nil {
		return fmt.Errorf("解析文件路径失败：%w", err)
	}
	book.Filename = p
	return nil
}

func FlagParse() *Book {
	var book Book
	flag.StringVar(&book.Name, "name", "", "书名，不填写程序会自动解析")
	flag.StringVar(&book.Author, "author", "", "作者，不填写程序会自动解析")
	flag.StringVar(&book.Lang, "lang", "", "语言，默认中文，可设置其他语言如：en,de,fr,it,es,zh,ja,pt,ru,nl")
	flag.StringVar(&book.Cover, "cover", "", "封面图片的路径，可以是本地文件路径也可以是网络图片url")
	flag.StringVar(&book.Intro, "intro", "", "简介，不填写程序会自动解析")
	flag.StringVar(&book.Publisher, "publisher", "", "出版社")
	flag.StringVar(&book.PublishDate, "date", "", "出版日期")
	flag.StringVar(&book.Filename, "file", "", "文件路径")
	flag.StringVar(&book.Output, "output", "", "输出路径")
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
	} else {
		book.VolumeRegex = regexp.MustCompile(VolumePattern)
	}
	if chapterPattern != "" {
		book.ChapterRegex = regexp.MustCompile(chapterPattern)
	} else {
		book.ChapterRegex = regexp.MustCompile(ChapterPattern)
	}
	if extraPattern != "" {
		book.ExtraRegex = regexp.MustCompile(extraPattern)
	} else {
		book.ExtraRegex = regexp.MustCompile(ExtraPattern)
	}

	if introPattern != "" {
		book.IntroRegex = regexp.MustCompile(introPattern)
	} else {
		book.IntroRegex = regexp.MustCompile(IntroPattern)
	}
	return &book
}
