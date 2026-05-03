package goepub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func isolateAutoRuleConfigDiscovery(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	oldExecFunc := executablePathFunc
	oldUserConfigFunc := userConfigDirFunc

	executablePathFunc = func() (string, error) {
		return filepath.Join(tmpDir, "bin", "gotexttoepub-test"), nil
	}
	userConfigDirFunc = func() (string, error) {
		return filepath.Join(tmpDir, "user-config"), nil
	}

	t.Cleanup(func() {
		executablePathFunc = oldExecFunc
		userConfigDirFunc = oldUserConfigFunc
	})
}

func TestBuildParseRulesUsesBuiltinIgnoreRules(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	rules, err := buildParseRules(&Book{})
	if err != nil {
		t.Fatalf("build parse rules: %v", err)
	}

	authorNote := "第一卷接近尾声，三九要再整理一遍后面的大纲，今天只有两更~"
	if !rules.ShouldIgnoreLine(authorNote) {
		t.Fatalf("expected author note to be ignored: %s", authorNote)
	}

	promoSentence := "第一卷：这个时代，人类渺小如尘埃。"
	if !rules.ShouldIgnoreLine(promoSentence) {
		t.Fatalf("expected prose-like volume sentence to be ignored: %s", promoSentence)
	}

	realVolume := "第一卷 灰界降临"
	if rules.ShouldIgnoreLine(realVolume) {
		t.Fatalf("expected real volume title not to be ignored: %s", realVolume)
	}

	realVolumeWithColon := "第一卷：灰界降临"
	if rules.ShouldIgnoreLine(realVolumeWithColon) {
		t.Fatalf("expected real volume title with colon not to be ignored: %s", realVolumeWithColon)
	}
}

func TestBuildParseRulesCanLoadConfigOverride(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rules.toml")
	configContent := `
# 这是一份带注释的规则覆盖示例。
ignored_line_patterns = [
  "^作者的话.*$",
]

ignored_line_contains = [
  "临时通知",
]

intro_prefixes = [
  "小说简介：",
]
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rules, err := buildParseRules(&Book{RuleConfigPath: configPath})
	if err != nil {
		t.Fatalf("build parse rules from config: %v", err)
	}

	if !rules.ShouldIgnoreLine("作者的话：今晚请假") {
		t.Fatalf("expected config pattern to ignore author-note line")
	}
	if !rules.ShouldIgnoreLine("平台临时通知：今晚延迟更新") {
		t.Fatalf("expected config keyword to ignore line")
	}

	intro, ok := rules.ParsePrefixedIntro("小说简介：这是一本测试小说。")
	if !ok || intro != "这是一本测试小说。" {
		t.Fatalf("expected custom intro prefix to be loaded, got ok=%v intro=%q", ok, intro)
	}
}

func TestBuildParseRulesCanApplyNamedPreset(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	rules, err := buildParseRules(&Book{
		RulePresets: []string{"qidian", "serial"},
	})
	if err != nil {
		t.Fatalf("build parse rules with presets: %v", err)
	}

	if !rules.ShouldIgnoreLine("上架感言：感谢大家支持") {
		t.Fatalf("expected qidian preset to ignore shangjia note")
	}
	if !rules.ShouldIgnoreLine("作者的话：今晚请假") {
		t.Fatalf("expected serial preset to ignore author note")
	}
}

func TestBuildParseRulesSupportsConfigExtendedPreset(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "preset-rules.toml")
	configContent := `
extends_presets = [
  "jjwxc",
]
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rules, err := buildParseRules(&Book{RuleConfigPath: configPath})
	if err != nil {
		t.Fatalf("build parse rules from preset config: %v", err)
	}

	if !rules.ShouldIgnoreLine("入V公告：本文明天入V") {
		t.Fatalf("expected jjwxc preset from config to ignore v notice")
	}

	intro, ok := rules.ParsePrefixedIntro("文案：这是测试文案。")
	if !ok || intro != "这是测试文案。" {
		t.Fatalf("expected jjwxc preset intro prefix to be loaded, got ok=%v intro=%q", ok, intro)
	}
}

func TestBuildParseRulesRejectsUnknownPreset(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	_, err := buildParseRules(&Book{
		RulePresets: []string{"unknown"},
	})
	if err == nil {
		t.Fatal("expected unknown preset to return error")
	}
}

func TestLoadRuleConfigRejectsUnknownField(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid-rules.toml")
	configContent := `
unknown_field = "oops"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := loadRuleConfig(configPath, "")
	if err == nil {
		t.Fatal("expected unknown field to return error")
	}
}

func TestBuildParseRulesCanAutoLoadUserAndProgramConfig(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	userConfigRoot := filepath.Join(tmpDir, "user-config")
	execDir := filepath.Join(tmpDir, "bin")

	if err := os.MkdirAll(filepath.Join(userConfigRoot, defaultConfigDirName), 0o755); err != nil {
		t.Fatalf("mkdir user config: %v", err)
	}
	if err := os.MkdirAll(execDir, 0o755); err != nil {
		t.Fatalf("mkdir exec dir: %v", err)
	}

	userConfigPath := filepath.Join(userConfigRoot, defaultConfigDirName, defaultUserRuleConfigName)
	if err := os.WriteFile(userConfigPath, []byte("intro_prefixes = [\"全局简介：\"]\n"), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	programConfigPath := filepath.Join(execDir, defaultProgramRuleConfigName)
	if err := os.WriteFile(programConfigPath, []byte("title_regex = \"^书名[:：](.*)$\"\n"), 0o644); err != nil {
		t.Fatalf("write program config: %v", err)
	}

	oldExecFunc := executablePathFunc
	oldUserConfigFunc := userConfigDirFunc
	executablePathFunc = func() (string, error) {
		return filepath.Join(execDir, "gotexttoepub.exe"), nil
	}
	userConfigDirFunc = func() (string, error) {
		return userConfigRoot, nil
	}
	defer func() {
		executablePathFunc = oldExecFunc
		userConfigDirFunc = oldUserConfigFunc
	}()

	rules, err := buildParseRules(&Book{})
	if err != nil {
		t.Fatalf("build parse rules from auto config: %v", err)
	}

	title, ok := rules.ParseTitle("书名：自动加载标题")
	if !ok || title != "自动加载标题" {
		t.Fatalf("expected title regex from program dir config, got ok=%v title=%q", ok, title)
	}

	intro, ok := rules.ParsePrefixedIntro("全局简介：自动加载简介")
	if !ok || intro != "自动加载简介" {
		t.Fatalf("expected intro prefix from user config dir, got ok=%v intro=%q", ok, intro)
	}
}

func TestLoadRuleConfigSupportsChannelsAndDefaultChannel(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "channel-rules.toml")
	configContent := `
default_channel = "fanqie"
title_regex = "^书名[:：](.*)$"

[channels.qidian]
author_regex = "^起点作者[:：](.*)$"
chapter_regex = "^第[0-9]+章.*$"

[channels.fanqie]
author_regex = "^番茄作者[:：](.*)$"
chapter_regex = "^Chapter\\s+[0-9]+.*$"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	qidianCfg, qidianChannel, err := loadRuleConfig(configPath, "qidian")
	if err != nil {
		t.Fatalf("load qidian channel: %v", err)
	}
	if qidianChannel != "qidian" {
		t.Fatalf("expected selected channel qidian, got %q", qidianChannel)
	}
	if qidianCfg.AuthorRegex != "^起点作者[:：](.*)$" {
		t.Fatalf("unexpected qidian author regex: %s", qidianCfg.AuthorRegex)
	}
	if qidianCfg.TitleRegex != "^书名[:：](.*)$" {
		t.Fatalf("expected top-level title regex to be inherited, got %s", qidianCfg.TitleRegex)
	}

	defaultCfg, defaultChannel, err := loadRuleConfig(configPath, "")
	if err != nil {
		t.Fatalf("load default channel: %v", err)
	}
	if defaultChannel != "fanqie" {
		t.Fatalf("expected selected default channel fanqie, got %q", defaultChannel)
	}
	if defaultCfg.AuthorRegex != "^番茄作者[:：](.*)$" {
		t.Fatalf("unexpected default author regex: %s", defaultCfg.AuthorRegex)
	}
}

func TestLoadRuleConfigRejectsUnknownChannel(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "channel-rules.toml")
	configContent := `
[channels.qidian]
author_regex = "^起点作者[:：](.*)$"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := loadRuleConfig(configPath, "fanqie")
	if err == nil {
		t.Fatal("expected unknown channel to return error")
	}
}

func TestListRuleConfigSummaries(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rules.toml")
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

	summaries, err := ListRuleConfigSummaries(configPath)
	if err != nil {
		t.Fatalf("list rule config summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].DefaultChannel != "qidian" {
		t.Fatalf("expected default channel qidian, got %q", summaries[0].DefaultChannel)
	}
	if len(summaries[0].Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(summaries[0].Channels))
	}
	qidianDetail, ok := summaries[0].ChannelDetails["qidian"]
	if !ok {
		t.Fatal("expected qidian channel details to exist")
	}
	if len(qidianDetail.DefinedFields) == 0 || qidianDetail.DefinedFields[0] != "author_regex" {
		t.Fatalf("expected qidian detail fields to include author_regex, got %v", qidianDetail.DefinedFields)
	}
}

func TestResolveEffectiveRuleConfigSummary(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rules.toml")
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

	summary, err := ResolveEffectiveRuleConfigSummary(configPath, "")
	if err != nil {
		t.Fatalf("resolve effective rule config summary: %v", err)
	}
	if len(summary.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(summary.Sources))
	}
	if summary.Sources[0].SelectedChannel != "qidian" {
		t.Fatalf("expected selected channel qidian, got %q", summary.Sources[0].SelectedChannel)
	}
	if summary.Config.AuthorRegex != "^起点作者[:：](.*)$" {
		t.Fatalf("unexpected author regex: %s", summary.Config.AuthorRegex)
	}
	if summary.Config.ChapterRegex != "^第[0-9]+章.*$" {
		t.Fatalf("unexpected chapter regex: %s", summary.Config.ChapterRegex)
	}
	if len(summary.Config.IntroPrefixes) == 0 || summary.Config.IntroPrefixes[0] != "全局简介：" {
		t.Fatalf("expected global intro prefix to be kept, got %v", summary.Config.IntroPrefixes)
	}
	if !containsString(summary.Config.IgnoredLineContains, "求月票") {
		t.Fatalf("expected qidian preset rules to be applied, got %v", summary.Config.IgnoredLineContains)
	}
}

func TestParseCanUseCustomTitleAndAuthorRegex(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "custom-story.txt")
	outputPath := filepath.Join(tmpDir, "custom-story.epub")
	configPath := filepath.Join(tmpDir, "custom-rules.toml")

	content := `书名：测试小说
作者名：李四
第一章 开始
正文内容`

	config := `
title_regex = "^书名[:：](.*)$"
author_regex = "^作者名[:：](.*)$"
`

	if err := os.WriteFile(txtPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	book := &Book{
		Filename:       txtPath,
		Output:         outputPath,
		RuleConfigPath: configPath,
	}
	if err := NewEPUBConverter().Convert(context.Background(), book); err != nil {
		t.Fatalf("convert: %v", err)
	}

	if book.Name != "测试小说" {
		t.Fatalf("unexpected title: %s", book.Name)
	}
	if book.Author != "李四" {
		t.Fatalf("unexpected author: %s", book.Author)
	}
}

func TestParseCanUseCustomTitleAuthorVolumeAndChapterRegexFromConfig(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "configured-story.txt")
	outputPath := filepath.Join(tmpDir, "configured-story.epub")
	configPath := filepath.Join(tmpDir, "configured-rules.toml")

	content := `《测试小说》 作者：王五
正文卷 开篇
Chapter 1 初见
正文内容`

	config := `
title_author_regex = "^《(.+?)》\\s+作者[:：]\\s*(.+)$"
volume_regex = "^正文卷.*$"
chapter_regex = "^Chapter\\s+[0-9]+.*$"
`

	if err := os.WriteFile(txtPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	book := &Book{
		Filename:       txtPath,
		Output:         outputPath,
		RuleConfigPath: configPath,
	}
	if err := NewEPUBConverter().Convert(context.Background(), book); err != nil {
		t.Fatalf("convert: %v", err)
	}

	if book.Name != "测试小说" {
		t.Fatalf("unexpected title: %s", book.Name)
	}
	if book.Author != "王五" {
		t.Fatalf("unexpected author: %s", book.Author)
	}
	if len(book.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(book.Volumes))
	}
	if book.Volumes[0].Title != "正文卷 开篇" {
		t.Fatalf("unexpected volume title: %s", book.Volumes[0].Title)
	}
	if len(book.Volumes[0].Chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(book.Volumes[0].Chapters))
	}
	if book.Volumes[0].Chapters[0].Title != "Chapter 1 初见" {
		t.Fatalf("unexpected chapter title: %s", book.Volumes[0].Chapters[0].Title)
	}
}

func TestDetectRulePresetsFindsQidianAndSerial(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	text := `
上架感言：感谢大家一路追读
单章求月票
作者的话：今晚请假
`

	detections := DetectRulePresets(text)
	if len(detections) < 2 {
		t.Fatalf("expected at least two preset detections, got %d", len(detections))
	}

	foundQidian := false
	foundSerial := false
	for _, detected := range detections {
		if detected.Name == "qidian" {
			foundQidian = true
		}
		if detected.Name == "serial" {
			foundSerial = true
		}
	}

	if !foundQidian {
		t.Fatal("expected qidian preset to be detected")
	}
	if !foundSerial {
		t.Fatal("expected serial preset to be detected")
	}
}

func TestConvertCanAutoApplyDetectedPreset(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "story.txt")
	outputPath := filepath.Join(tmpDir, "story.epub")
	content := `测试小说
文案：这是一本用于测试自动预设探测的小说。
第一章 开始
正文内容`

	if err := os.WriteFile(txtPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	book := &Book{
		Filename:       txtPath,
		Output:         outputPath,
		RulePresetMode: presetModeApply,
	}
	if err := NewEPUBConverter().Convert(context.Background(), book); err != nil {
		t.Fatalf("convert: %v", err)
	}

	if book.Intro != "这是一本用于测试自动预设探测的小说。" {
		t.Fatalf("expected detected jjwxc preset to parse intro, got %q", book.Intro)
	}
	if len(book.detectedRulePresets) == 0 || book.detectedRulePresets[0] != "jjwxc" {
		t.Fatalf("expected jjwxc preset to be auto applied, got %v", book.detectedRulePresets)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
