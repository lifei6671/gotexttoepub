package cmd

import (
	"github.com/lifei6671/gotexttoepub/goepub"
	"gopkg.in/urfave/cli.v2"
	"log"
	"regexp"
	"time"
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
			Name:    "regexr",
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
	},
	Action: func(c *cli.Context) error {

		path := c.String("file")
		if path == "" {
			log.Fatal("文件路径不能为空")
		}
		epub := goepub.NewConverter()
		epub.SetCover(c.String("cover"))
		regexr := c.String("regexr")
		if regexr == "" {
			regexr = `(^第.*?章.*)`
		}
		reg := regexp.MustCompile(regexr)

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
