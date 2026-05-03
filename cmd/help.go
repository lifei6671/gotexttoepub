package cmd

import (
	"github.com/urfave/cli/v2"
)

const chineseAppHelpTemplate = `名称:
   {{template "helpNameTemplate" .}}

用法:
   {{if .UsageText}}{{wrap .UsageText 3}}{{else}}{{.HelpName}} {{if .VisibleFlags}}[全局参数]{{end}}{{if .Commands}} 命令 [命令参数]{{end}}{{if .ArgsUsage}} {{.ArgsUsage}}{{else}}{{if .Args}} [参数...]{{end}}{{end}}{{end}}{{if .Version}}{{if not .HideVersion}}

版本:
   {{.Version}}{{end}}{{end}}{{if .Description}}

说明:
   {{template "descriptionTemplate" .}}{{end}}
{{- if len .Authors}}

作者{{template "authorsTemplate" .}}{{end}}{{if .VisibleCommands}}

命令:{{template "visibleCommandCategoryTemplate" .}}{{end}}{{if .VisibleFlagCategories}}

全局参数:{{template "visibleFlagCategoryTemplate" .}}{{else if .VisibleFlags}}

全局参数:{{template "visibleFlagTemplate" .}}{{end}}{{if .Copyright}}

版权:
   {{template "copyrightTemplate" .}}{{end}}
`

const chineseCommandHelpTemplate = `名称:
   {{template "helpNameTemplate" .}}

用法:
   {{if .UsageText}}{{wrap .UsageText 3}}{{else}}{{.HelpName}}{{if .VisibleFlags}} [命令参数]{{end}}{{if .ArgsUsage}} {{.ArgsUsage}}{{else}}{{if .Args}} [参数...]{{end}}{{end}}{{end}}{{if .Category}}

分类:
   {{.Category}}{{end}}{{if .Description}}

说明:
   {{template "descriptionTemplate" .}}{{end}}{{if .VisibleFlagCategories}}

参数:{{template "visibleFlagCategoryTemplate" .}}{{else if .VisibleFlags}}

参数:{{template "visibleFlagTemplate" .}}{{end}}
`

const chineseSubcommandHelpTemplate = `名称:
   {{template "helpNameTemplate" .}}

用法:
   {{if .UsageText}}{{wrap .UsageText 3}}{{else}}{{.HelpName}} {{if .VisibleFlags}}命令 [命令参数]{{end}}{{if .ArgsUsage}} {{.ArgsUsage}}{{else}}{{if .Args}} [参数...]{{end}}{{end}}{{end}}{{if .Description}}

说明:
   {{template "descriptionTemplate" .}}{{end}}{{if .VisibleCommands}}

命令:{{template "visibleCommandCategoryTemplate" .}}{{end}}{{if .VisibleFlagCategories}}

参数:{{template "visibleFlagCategoryTemplate" .}}{{else if .VisibleFlags}}

参数:{{template "visibleFlagTemplate" .}}{{end}}
`

// ConfigureCLIHelp 统一配置中文帮助模板与帮助参数文案。
func ConfigureCLIHelp(app *cli.App) {
	if app == nil {
		return
	}

	cli.HelpFlag = &cli.BoolFlag{
		Name:               "help",
		Aliases:            []string{"h"},
		Usage:              "显示帮助",
		DisableDefaultText: true,
	}
	cli.VersionFlag = &cli.BoolFlag{
		Name:               "version",
		Aliases:            []string{"v"},
		Usage:              "显示版本",
		DisableDefaultText: true,
	}

	app.CustomAppHelpTemplate = chineseAppHelpTemplate
	app.HideHelpCommand = true

	for _, command := range app.Commands {
		configureCommandHelp(command)
	}
}

func configureCommandHelp(command *cli.Command) {
	if command == nil {
		return
	}

	command.HideHelpCommand = true
	if len(command.Subcommands) > 0 {
		command.CustomHelpTemplate = chineseSubcommandHelpTemplate
	} else {
		command.CustomHelpTemplate = chineseCommandHelpTemplate
	}

	for _, subcommand := range command.Subcommands {
		configureCommandHelp(subcommand)
	}
}
