package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/lifei6671/gotexttoepub/goepub"
)

// RulesCommand 提供规则配置相关的辅助命令。
var RulesCommand = newRulesCommand()

func newRulesCommand() *cli.Command {
	return &cli.Command{
		Name:        "rules",
		Usage:       "查看规则文件与可用渠道",
		Description: "列出当前规则文件中的可用渠道，方便在转换前选择 rule-channel。",
		Subcommands: []*cli.Command{
		{
			Name:  "channels",
			Usage: "列出可用规则渠道",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "rule-config",
					Aliases: []string{"config"},
					Usage:   "显式指定规则配置文件；留空则自动扫描默认规则文件位置",
				},
				&cli.BoolFlag{
					Name:  "show-details",
					Usage: "显示每个渠道继承的预设和已定义的规则字段",
				},
			},
			Action: func(c *cli.Context) error {
				summaries, err := goepub.ListRuleConfigSummaries(c.String("rule-config"))
				if err != nil {
					return err
				}
				writer := c.App.Writer
				if writer == nil {
					writer = os.Stdout
				}
				if len(summaries) == 0 {
					fmt.Fprintln(writer, "未找到可用的规则配置文件。")
					return nil
				}

				for _, summary := range summaries {
					fmt.Fprintf(writer, "规则文件: %s\n", summary.Path)
					if summary.DefaultChannel != "" {
						fmt.Fprintf(writer, "默认渠道: %s\n", summary.DefaultChannel)
					} else {
						fmt.Fprintln(writer, "默认渠道: 未设置")
					}

					if len(summary.Channels) == 0 {
						fmt.Fprintln(writer, "可用渠道: 无，仅顶层全局规则")
						fmt.Fprintln(writer)
						continue
					}

					channels := append([]string(nil), summary.Channels...)
					sort.Strings(channels)
					lines := make([]string, 0, len(channels))
					for _, channel := range channels {
						label := channel
						if strings.EqualFold(channel, summary.DefaultChannel) {
							label += " (default)"
						}
						lines = append(lines, label)
					}

					fmt.Fprintf(writer, "可用渠道: %s\n", strings.Join(lines, ", "))
					if c.Bool("show-details") {
						for _, channel := range channels {
							detail := summary.ChannelDetails[channel]
							fmt.Fprintf(writer, "  - %s\n", channel)
							if len(detail.ExtendsPresets) > 0 {
								fmt.Fprintf(writer, "    presets: %s\n", strings.Join(detail.ExtendsPresets, ", "))
							} else {
								fmt.Fprintf(writer, "    presets: 无\n")
							}
							if len(detail.DefinedFields) > 0 {
								fmt.Fprintf(writer, "    fields: %s\n", strings.Join(detail.DefinedFields, ", "))
							} else {
								fmt.Fprintf(writer, "    fields: 无\n")
							}
						}
					}
					fmt.Fprintln(writer)
				}
				return nil
			},
		},
		{
			Name:  "show",
			Usage: "显示某个渠道最终生效的完整规则",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "rule-config",
					Aliases: []string{"config"},
					Usage:   "显式指定规则配置文件；留空则按自动发现顺序合并规则文件",
				},
				&cli.StringFlag{
					Name:    "rule-channel",
					Aliases: []string{"channel"},
					Usage:   "指定要查看的渠道名称；留空则使用默认渠道",
				},
			},
			Action: func(c *cli.Context) error {
				summary, err := goepub.ResolveEffectiveRuleConfigSummary(c.String("rule-config"), c.String("rule-channel"))
				if err != nil {
					return err
				}

				writer := c.App.Writer
				if writer == nil {
					writer = os.Stdout
				}

				fmt.Fprintln(writer, "规则来源:")
				fmt.Fprintln(writer, "- 内置默认规则")
				if len(summary.Sources) == 0 {
					fmt.Fprintln(writer, "- 无额外规则文件")
				} else {
					for _, source := range summary.Sources {
						if source.SelectedChannel != "" {
							fmt.Fprintf(writer, "- %s [channel=%s]\n", source.Path, source.SelectedChannel)
						} else {
							fmt.Fprintf(writer, "- %s [channel=顶层全局规则]\n", source.Path)
						}
					}
				}
				fmt.Fprintln(writer)

				printRuleField(writer, "title_regex", summary.Config.TitleRegex)
				printRuleField(writer, "title_author_regex", summary.Config.TitleAuthorRegex)
				printRuleField(writer, "author_regex", summary.Config.AuthorRegex)
				printRuleField(writer, "volume_regex", summary.Config.VolumeRegex)
				printRuleField(writer, "chapter_regex", summary.Config.ChapterRegex)
				printRuleField(writer, "extra_regex", summary.Config.ExtraRegex)
				printRuleField(writer, "intro_regex", summary.Config.IntroRegex)
				printRuleList(writer, "intro_prefixes", summary.Config.IntroPrefixes)
				printRuleList(writer, "special_chapter_titles", summary.Config.SpecialChapterTitles)
				printRuleList(writer, "ignored_line_patterns", summary.Config.IgnoredLinePatterns)
				printRuleList(writer, "ignored_line_contains", summary.Config.IgnoredLineContains)
				return nil
			},
		},
		},
	}
}

func printRuleField(writer io.Writer, name string, value string) {
	fmt.Fprintf(writer, "%s: %s\n", name, value)
}

func printRuleList(writer io.Writer, name string, values []string) {
	if len(values) == 0 {
		fmt.Fprintf(writer, "%s: []\n", name)
		return
	}
	fmt.Fprintf(writer, "%s: %s\n", name, strings.Join(values, ", "))
}
