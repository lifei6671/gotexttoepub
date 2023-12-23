package main

import (
	"github.com/lifei6671/gotexttoepub/cmd"
	"gopkg.in/urfave/cli.v2"
	"log"
	"os"
)

const APP_VERSION = "1.1"

func main() {
	app := &cli.App{}
	app.Name = "gotexttoepub"
	app.Usage = "A Txt convert epub application."
	app.Version = APP_VERSION
	app.Commands = []*cli.Command{
		cmd.Start,
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatalf("启动命令行失败 -> %s", err)
	}
}

func init() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
}
