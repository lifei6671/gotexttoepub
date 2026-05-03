package main

import (
	"log"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/lifei6671/gotexttoepub/cmd"
)

const appVersion = "1.2"

// main 负责初始化 CLI 应用并分发子命令。
func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	app := &cli.App{
		Name:     "gotexttoepub",
		Usage:    "Convert TXT novels to EPUB files.",
		Version:  appVersion,
		Commands: []*cli.Command{cmd.Start, cmd.RulesCommand},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("启动命令行失败 -> %s", err)
	}
}
