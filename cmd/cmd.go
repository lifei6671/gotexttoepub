package cmd

import (
	"context"
	"log"
	"regexp"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/lifei6671/gotexttoepub/goepub"
)

var Start = &cli.Command{
	Name:        "epub",
	Usage:       "将一个TXT文件转换为epub格式",
	Description: `将一个TXT文件转换为epub格式，TXT文件需要有固定的格式，通过正则可分割成不同小说章节.`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "file",
			Aliases: []string{"f"},
			Value:   "",
			Usage:   "小说TXT文件路径",
		},
		&cli.StringFlag{
			Name:    "cover",
			Aliases: []string{"img"},
			Value:   "",
			Usage:   "小说封面",
		},
		&cli.StringFlag{
			Name:    "title-regexp",
			Aliases: []string{"r"},
			Value:   "",
			Usage:   "提取章节标题的正则",
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Value:   "",
			Usage:   "文件输出地址",
		},
		&cli.StringFlag{
			Name:    "volume-regexp",
			Aliases: []string{"vr"},
			Value:   "",
			Usage:   "提取章节标题的正则",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {

		path := c.String("file")
		if path == "" {
			log.Fatal("文件路径不能为空")
		}
		epub := goepub.NewConverter()
		epub.SetCover(c.String("cover"))
		regexr := c.String("title-regexp")
		if regexr == "" {
			regexr = goepub.ChapterPattern
		}
		reg := regexp.MustCompile(regexr)

		volumeRegStr := c.String("volume-regexp")
		if volumeRegStr == "" {
			volumeRegStr = goepub.VolumePattern
		}
		volumeReg := regexp.MustCompile(volumeRegStr)
		epub.SetVolumeReg(volumeReg)
		epub.SetRegExp(reg)
		epub.SetContent(path)

		start := time.Now()
		if err := epub.Convert(c.String("output")); err != nil {
			log.Fatalf("转换文档失败 -> %v", err)
		}

		log.Printf("转换完成,耗时 -> %d ms", time.Now().Sub(start).Nanoseconds()/1e6)
		return nil
	},
}
