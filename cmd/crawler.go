package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/lifei6671/gotexttoepub/spider"
)

var Crawler = &cli.Command{
	Name:        "crawler",
	Usage:       "从网络上抓取指定URL的小说，并转换为指定格式。",
	Description: `支持自定义小说源，通过自动抓取转换为epub或TXT等格式。`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "url",
			Aliases:  []string{"u"},
			Value:    "",
			Usage:    "小说的目录页面的网络URL地址",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "rule-path",
			Aliases: []string{"c"},
			Value:   "./conf/source.toml",
			Usage:   "抓取规则配置地址",
		},
		&cli.IntFlag{
			Name:    "start-chapter",
			Aliases: []string{"s"},
			Value:   0,
			Usage:   "抓取开始的章节，默认从第一章开始抓取",
		},
		&cli.IntFlag{
			Name:    "end-chapter",
			Aliases: []string{"e"},
			Value:   0,
			Usage:   "抓取结束的章节，默认全部抓取",
		},
		&cli.StringFlag{
			Name:     "output",
			Aliases:  []string{"o"},
			Usage:    "输出的默认路径",
			Value:    "",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "rule-name",
			Aliases: []string{"r"},
			Usage:   "抓取小说的使用规则，不指定则通过小说域名自动匹配",
		},
		&cli.StringFlag{
			Name:    "format",
			Aliases: []string{"f"},
			Usage:   "输出的格式，支持的有：txt、epub",
			Value:   "epub",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		param := &spider.CrawlerParams{
			ChapterURL:   c.String("url"),
			ChapterStart: c.Int("start-chapter"),
			ChapterEnd:   c.Int("end-chapter"),
			RuleName:     c.String("rule-name"),
		}
		if param.RuleName == "" {
			u, err := url.Parse(param.ChapterURL)
			if err != nil {
				return fmt.Errorf("解析小说地址失败：%s - %w", param.ChapterURL, err)
			}
			param.RuleName = u.Host
		}
		book, err := spider.Crawler(ctx, param)
		if err != nil {
			return err
		}

		wErr := os.WriteFile(c.String("output"), []byte(book.Text()), 0655)

		return wErr
	},
}
