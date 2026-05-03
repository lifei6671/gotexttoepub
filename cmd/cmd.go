package cmd

import (
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/lifei6671/gotexttoepub/goepub"
)

// Start 是 TXT 转 EPUB 的命令入口。
var Start = &cli.Command{
	Name:        "epub",
	Usage:       "将 TXT 小说转换为 EPUB",
	Description: "按卷、章节规则解析 TXT 文件并输出 EPUB。",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "file",
			Aliases:  []string{"f"},
			Required: true,
			Usage:    "TXT 文件路径",
		},
		&cli.StringFlag{
			Name:    "cover",
			Aliases: []string{"img"},
			Usage:   "封面图片路径或 URL",
		},
		&cli.StringFlag{
			Name:  "author",
			Usage: "作者，留空则自动解析",
		},
		&cli.StringFlag{
			Name:  "title-regexp",
			Usage: "书名解析正则，支持使用捕获组提取最终书名",
		},
		&cli.StringFlag{
			Name:  "author-regexp",
			Usage: "作者解析正则，支持使用捕获组提取最终作者名",
		},
		&cli.StringFlag{
			Name:  "lang",
			Usage: "语言，默认 zh-CN",
		},
		&cli.StringFlag{
			Name:    "encoding",
			Aliases: []string{"charset"},
			Usage:   "文本编码，默认 auto，可选 utf-8、gbk、gb18030",
		},
		&cli.StringFlag{
			Name:    "rule-config",
			Aliases: []string{"config"},
			Usage:   "规则配置文件路径，默认使用内置规则，可用带注释的 TOML 文件覆盖少量解析规则",
		},
		&cli.StringFlag{
			Name:    "rule-preset",
			Aliases: []string{"preset"},
			Usage:   "规则预设名称，多个预设使用逗号分隔，例如 qidian,serial",
		},
		&cli.StringFlag{
			Name:    "rule-channel",
			Aliases: []string{"channel"},
			Usage:   "规则渠道名称，例如 default、qidian、fanqie；留空则使用规则文件默认渠道",
		},
		&cli.StringFlag{
			Name:  "rule-preset-mode",
			Usage: "自动探测预设的行为模式：off、suggest、apply，默认 suggest",
		},
		&cli.StringFlag{
			Name:    "chapter-regexp",
			Aliases: []string{"r", "regexr", "title-regexp", "chapter-pattern"},
			Usage:   "提取章节标题的正则",
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "输出文件路径，或输出目录",
		},
		&cli.StringFlag{
			Name:    "volume-regexp",
			Aliases: []string{"vr", "volume-pattern"},
			Usage:   "提取卷标题的正则",
		},
	},
	Action: func(c *cli.Context) error {
		book, err := buildBookFromFlags(c)
		if err != nil {
			return err
		}

		start := time.Now()
		if err := goepub.NewEPUBConverter().Convert(c.Context, book); err != nil {
			return fmt.Errorf("转换文档失败: %w", err)
		}

		log.Printf("转换完成,耗时 -> %s", time.Since(start).Round(time.Millisecond))
		return nil
	},
}

// buildBookFromFlags 将命令行参数转换为统一的 Book 配置对象，
// 这样命令层只负责取参，实际转换逻辑全部下沉到 goepub 包。
func buildBookFromFlags(c *cli.Context) (*goepub.Book, error) {
	book := &goepub.Book{
		Filename:       c.String("file"),
		Cover:          c.String("cover"),
		Author:         c.String("author"),
		Lang:           c.String("lang"),
		Encoding:       c.String("encoding"),
		Output:         c.String("output"),
		RulePresets:    goepub.NormalizeRulePresetNames(c.String("rule-preset")),
		RuleChannel:    c.String("rule-channel"),
		RulePresetMode: c.String("rule-preset-mode"),
		RuleConfigPath: c.String("rule-config"),
	}

	titlePattern := c.String("title-regexp")
	if titlePattern != "" {
		titleRegex, err := regexp.Compile(titlePattern)
		if err != nil {
			return nil, fmt.Errorf("书名正则无效: %w", err)
		}
		book.TitleRegex = titleRegex
	}

	authorPattern := c.String("author-regexp")
	if authorPattern != "" {
		authorRegex, err := regexp.Compile(authorPattern)
		if err != nil {
			return nil, fmt.Errorf("作者正则无效: %w", err)
		}
		book.AuthorRegex = authorRegex
	}

	chapterPattern := c.String("chapter-regexp")
	if chapterPattern != "" {
		chapterRegex, err := regexp.Compile(chapterPattern)
		if err != nil {
			return nil, fmt.Errorf("章节正则无效: %w", err)
		}
		book.ChapterRegex = chapterRegex
	}

	volumePattern := c.String("volume-regexp")
	if volumePattern != "" {
		volumeRegex, err := regexp.Compile(volumePattern)
		if err != nil {
			return nil, fmt.Errorf("卷正则无效: %w", err)
		}
		book.VolumeRegex = volumeRegex
	}

	return book, nil
}
