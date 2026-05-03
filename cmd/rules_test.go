package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestRulesChannelsCommandPrintsConfiguredChannels(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/rules.toml"
	configContent := `
default_channel = "qidian"

[channels.qidian]
author_regex = "^起点作者[:：](.*)$"

[channels.fanqie]
author_regex = "^番茄作者[:：](.*)$"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var buffer bytes.Buffer
	app := &cli.App{
		Commands: []*cli.Command{RulesCommand},
		Writer:   &buffer,
	}

	if err := app.Run([]string{"gotexttoepub", "rules", "channels", "--rule-config", configPath}); err != nil {
		t.Fatalf("run cli app: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "qidian (default)") {
		t.Fatalf("expected output to contain default qidian channel, got: %s", output)
	}
	if !strings.Contains(output, "fanqie") {
		t.Fatalf("expected output to contain fanqie channel, got: %s", output)
	}
}

func TestRulesChannelsCommandShowDetails(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/rules.toml"
	configContent := `
default_channel = "qidian"

[channels.qidian]
extends_presets = ["qidian", "serial"]
author_regex = "^起点作者[:：](.*)$"
chapter_regex = "^第[0-9]+章.*$"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var buffer bytes.Buffer
	app := &cli.App{
		Commands: []*cli.Command{RulesCommand},
		Writer:   &buffer,
	}

	if err := app.Run([]string{"gotexttoepub", "rules", "channels", "--rule-config", configPath, "--show-details"}); err != nil {
		t.Fatalf("run cli app: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "presets: qidian, serial") {
		t.Fatalf("expected output to contain preset details, got: %s", output)
	}
	if !strings.Contains(output, "fields: author_regex, chapter_regex") {
		t.Fatalf("expected output to contain field details, got: %s", output)
	}
}

func TestRulesShowCommandPrintsEffectiveRules(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/rules.toml"
	configContent := `
default_channel = "qidian"
intro_prefixes = ["全局简介："]

[channels.qidian]
extends_presets = ["qidian"]
author_regex = "^起点作者[:：](.*)$"
chapter_regex = "^第[0-9]+章.*$"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var buffer bytes.Buffer
	app := &cli.App{
		Commands: []*cli.Command{RulesCommand},
		Writer:   &buffer,
	}

	if err := app.Run([]string{"gotexttoepub", "rules", "show", "--rule-config", configPath}); err != nil {
		t.Fatalf("run cli app: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "规则来源:") {
		t.Fatalf("expected output to contain sources section, got: %s", output)
	}
	if !strings.Contains(output, "author_regex: ^起点作者[:：](.*)$") {
		t.Fatalf("expected output to contain merged author regex, got: %s", output)
	}
	if !strings.Contains(output, "chapter_regex: ^第[0-9]+章.*$") {
		t.Fatalf("expected output to contain merged chapter regex, got: %s", output)
	}
	if !strings.Contains(output, "ignored_line_contains:") {
		t.Fatalf("expected output to contain final ignored_line_contains, got: %s", output)
	}
}
