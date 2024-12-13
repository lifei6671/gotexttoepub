package main

import (
	"context"
	"log"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/lifei6671/gotexttoepub/cmd"
)

const AppVersion = "2.0"

func main() {

	app := &cli.Command{
		Name:                  "gotexttoepub",
		Usage:                 "一个简单的小说抓取和转换程序",
		Version:               AppVersion,
		EnableShellCompletion: true,
		Commands: []*cli.Command{
			cmd.Start,
			cmd.Crawler,
		},
	}
	err := app.Run(context.TODO(), os.Args)
	if err != nil {
		log.Fatalf("启动命令行失败 -> %s", err)
	}
}

func init() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
}
