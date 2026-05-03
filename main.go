package main

import (
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/lifei6671/gotexttoepub/cmd"
)

const appVersion = "1.2"

// main 负责初始化 CLI 应用并分发子命令。
func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	commandNotFound := false
	app := &cli.App{
		Name:     "gotexttoepub",
		Usage:    "将 TXT 小说转换为 EPUB 文件。",
		Version:  appVersion,
		Commands: []*cli.Command{cmd.Start, cmd.RulesCommand},
		CommandNotFound: func(c *cli.Context, command string) {
			commandNotFound = true
			cmd.HandleCommandNotFound(c, command)
		},
		ErrWriter: os.Stderr,
	}
	cmd.ConfigureCLIHelp(app)

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, cmd.FormatCLIError(err))
		os.Exit(cmd.CLIExitCode(err))
	}
	if commandNotFound {
		os.Exit(3)
	}
}
