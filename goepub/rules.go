package goepub

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const defaultTitleAuthorPattern = `^(.+?)\s+作者[:：]\s*(.+)$`

const (
	defaultProgramRuleConfigName = "rules.toml"
	defaultUserRuleConfigName    = "rules.toml"
	defaultConfigDirName         = "gotexttoepub"
)

var (
	executablePathFunc = os.Executable
	userConfigDirFunc  = os.UserConfigDir
)

var defaultAuthorNoteContains = []string{
	"今天只有",
	"请假",
	"请一天假",
	"整理大纲",
	"加更",
	"两更",
	"三更",
	"卡文",
	"晚点更",
	"更新说明",
	"月底请假",
	"卷接近尾声",
}

// BuiltinRulePreset 描述一个内置命名规则预设。
// 预设用于在通用内置规则之上，补充少量来源或站点常见的格式特征。
type BuiltinRulePreset struct {
	Name        string
	Description string
	Config      RuleConfig
	Detector    PresetDetector
}

// PresetDetector 描述一个规则预设的自动探测条件。
type PresetDetector struct {
	Contains     []string
	Regexps      []string
	MinimumScore int
}

// DetectedRulePreset 表示一次自动探测得到的预设候选及命中依据。
type DetectedRulePreset struct {
	Name    string
	Score   int
	Reasons []string
}

const (
	presetModeOff     = "off"
	presetModeSuggest = "suggest"
	presetModeApply   = "apply"
)

var builtinRulePresets = map[string]BuiltinRulePreset{
	"serial": {
		Name:        "serial",
		Description: "连载小说通用预设，补充常见作者说明、更新通知与单章提示规则。",
		Config: RuleConfig{
			IgnoredLinePatterns: []string{
				`^作者的话[:：]?.*$`,
				`^请假条[:：]?.*$`,
				`^加更规则[:：]?.*$`,
				`^单章[:：]?.*$`,
				`^更新时间说明[:：]?.*$`,
			},
			IgnoredLineContains: []string{
				"作者的话",
				"更新时间说明",
				"加更规则",
				"今晚请假",
				"明天恢复更新",
			},
		},
		Detector: PresetDetector{
			Contains: []string{
				"作者的话",
				"请假条",
				"加更规则",
				"更新时间说明",
			},
			Regexps: []string{
				`^作者的话[:：]?.*$`,
				`^请假条[:：]?.*$`,
				`^加更规则[:：]?.*$`,
			},
			MinimumScore: 2,
		},
	},
	"qidian": {
		Name:        "qidian",
		Description: "起点类文本预设，补充上架感言、求票、单章等常见提示行规则。",
		Config: RuleConfig{
			IgnoredLinePatterns: []string{
				`^上架感言.*$`,
				`^求(月票|推荐票|订阅).*$`,
				`^单章求(月票|推荐票|订阅).*$`,
			},
			IgnoredLineContains: []string{
				"求月票",
				"求推荐票",
				"求订阅",
				"首订",
				"均订",
			},
		},
		Detector: PresetDetector{
			Contains: []string{
				"求月票",
				"求推荐票",
				"求订阅",
				"首订",
				"均订",
			},
			Regexps: []string{
				`^上架感言.*$`,
				`^单章求(月票|推荐票|订阅).*$`,
				`^求(月票|推荐票|订阅).*$`,
			},
			MinimumScore: 2,
		},
	},
	"fanqie": {
		Name:        "fanqie",
		Description: "番茄类文本预设，补充作者说明、催更提示和更新通知规则。",
		Config: RuleConfig{
			IgnoredLinePatterns: []string{
				`^催更说明.*$`,
				`^更新通知.*$`,
				`^作者说明.*$`,
			},
			IgnoredLineContains: []string{
				"催更",
				"更新通知",
				"作者说明",
			},
		},
		Detector: PresetDetector{
			Contains: []string{
				"催更",
				"更新通知",
				"作者说明",
			},
			Regexps: []string{
				`^催更说明.*$`,
				`^更新通知.*$`,
				`^作者说明.*$`,
			},
			MinimumScore: 2,
		},
	},
	"jjwxc": {
		Name:        "jjwxc",
		Description: "晋江类文本预设，补充入V公告、谢绝扒榜和阅读提示规则。",
		Config: RuleConfig{
			IntroPrefixes: []string{
				"文案：",
				"文案:",
			},
			IgnoredLinePatterns: []string{
				`^入V公告.*$`,
				`^谢绝扒榜.*$`,
				`^阅读提示[:：]?.*$`,
			},
			IgnoredLineContains: []string{
				"入V公告",
				"谢绝扒榜",
			},
		},
		Detector: PresetDetector{
			Contains: []string{
				"入V公告",
				"谢绝扒榜",
				"文案：",
				"文案:",
			},
			Regexps: []string{
				`^入V公告.*$`,
				`^谢绝扒榜.*$`,
				`^阅读提示[:：]?.*$`,
			},
			MinimumScore: 1,
		},
	},
}

// RuleConfig 是规则配置文件的可序列化结构。
// 大多数场景直接使用内置规则即可，只有少量特殊文本才需要用配置文件覆盖。
type RuleConfig struct {
	ExtendsPresets       []string `json:"extends_presets" toml:"extends_presets"`
	TitleRegex           string   `json:"title_regex" toml:"title_regex"`
	TitleAuthorRegex     string   `json:"title_author_regex" toml:"title_author_regex"`
	AuthorRegex          string   `json:"author_regex" toml:"author_regex"`
	VolumeRegex          string   `json:"volume_regex" toml:"volume_regex"`
	ChapterRegex         string   `json:"chapter_regex" toml:"chapter_regex"`
	ExtraRegex           string   `json:"extra_regex" toml:"extra_regex"`
	IntroRegex           string   `json:"intro_regex" toml:"intro_regex"`
	IntroPrefixes        []string `json:"intro_prefixes" toml:"intro_prefixes"`
	SpecialChapterTitles []string `json:"special_chapter_titles" toml:"special_chapter_titles"`
	IgnoredLinePatterns  []string `json:"ignored_line_patterns" toml:"ignored_line_patterns"`
	IgnoredLineContains  []string `json:"ignored_line_contains" toml:"ignored_line_contains"`
}

// RuleFileConfig 描述完整的规则文件结构。
// 顶层规则会作为全局基础规则，再根据渠道名称叠加 channels 中的对应配置。
type RuleFileConfig struct {
	DefaultChannel string                `toml:"default_channel"`
	Channels       map[string]RuleConfig `toml:"channels"`
	RuleConfig
}

// RuleConfigSummary 用于向外暴露规则文件中的渠道概览。
type RuleConfigSummary struct {
	Path           string
	DefaultChannel string
	Channels       []string
	ChannelDetails map[string]RuleChannelSummary
}

// RuleChannelSummary 描述某个渠道块里定义了哪些规则项。
type RuleChannelSummary struct {
	ExtendsPresets []string
	DefinedFields  []string
}

// ResolvedRuleSource 描述一层最终参与合并的规则来源。
type ResolvedRuleSource struct {
	Path            string
	SelectedChannel string
}

// ResolvedRuleConfigSummary 描述最终生效的完整规则。
// 它反映的是“内置默认规则 + 自动/显式规则文件 + 选中渠道”合并后的结果。
type ResolvedRuleConfigSummary struct {
	Sources []ResolvedRuleSource
	Config  RuleConfig
}

// ParseRules 是解析时实际使用的规则集合。
// 它由“内置规则 + 配置文件覆盖 + 命令行/代码直接传入的正则”共同构造。
type ParseRules struct {
	TitleRegex          *regexp.Regexp
	TitleAuthorRegex    *regexp.Regexp
	AuthorRegex         *regexp.Regexp
	VolumeRegex         *regexp.Regexp
	ChapterRegex        *regexp.Regexp
	ExtraRegex          *regexp.Regexp
	IntroRegex          *regexp.Regexp
	IntroPrefixes       []string
	SpecialChapterSet   map[string]struct{}
	IgnoredLineRegexps  []*regexp.Regexp
	IgnoredLineContains []string
}

// buildParseRules 组合内置规则、配置文件规则和代码直接传入的覆盖项。
func buildParseRules(book *Book) (*ParseRules, error) {
	cfg := defaultRuleConfig()

	var err error
	cfg, err = applyRulePresets(cfg, book.RulePresets)
	if err != nil {
		return nil, err
	}
	cfg, err = applyRulePresets(cfg, book.detectedRulePresets)
	if err != nil {
		return nil, err
	}

	autoConfigs, err := loadAutoRuleConfigs(book.RuleChannel)
	if err != nil {
		return nil, err
	}
	cfg, err = mergeLoadedRuleConfigs(cfg, autoConfigs)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(book.RuleConfigPath) != "" {
		userCfg, _, err := loadRuleConfig(book.RuleConfigPath, book.RuleChannel)
		if err != nil {
			return nil, err
		}

		cfg, err = applyRulePresets(cfg, userCfg.ExtendsPresets)
		if err != nil {
			return nil, err
		}
		cfg = mergeRuleConfig(cfg, userCfg)
	}

	if book.VolumeRegex != nil {
		cfg.VolumeRegex = book.VolumeRegex.String()
	}
	if book.TitleRegex != nil {
		cfg.TitleRegex = book.TitleRegex.String()
	}
	if book.AuthorRegex != nil {
		cfg.AuthorRegex = book.AuthorRegex.String()
	}
	if book.ChapterRegex != nil {
		cfg.ChapterRegex = book.ChapterRegex.String()
	}
	if book.ExtraRegex != nil {
		cfg.ExtraRegex = book.ExtraRegex.String()
	}
	if book.IntroRegex != nil {
		cfg.IntroRegex = book.IntroRegex.String()
	}

	return compileRuleConfig(cfg)
}

// NormalizeRulePresetNames 将逗号分隔的预设名标准化为去重后的切片。
func NormalizeRulePresetNames(value string) []string {
	return normalizeRulePresetNames(value)
}

func normalizePresetMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", presetModeSuggest:
		return presetModeSuggest
	case presetModeOff:
		return presetModeOff
	case presetModeApply:
		return presetModeApply
	default:
		return presetModeSuggest
	}
}

func normalizeRulePresetNames(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

// AvailableRulePresets 返回当前内置规则预设的名称列表。
func AvailableRulePresets() []string {
	names := make([]string, 0, len(builtinRulePresets))
	for name := range builtinRulePresets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// defaultRuleConfig 返回项目内置规则。
// 这些规则覆盖绝大多数通用中文小说 TXT 的解析场景。
func defaultRuleConfig() RuleConfig {
	return RuleConfig{
		TitleRegex:       TitlePattern,
		TitleAuthorRegex: defaultTitleAuthorPattern,
		AuthorRegex:      AuthorPattern,
		VolumeRegex:      VolumePattern,
		ChapterRegex:     ChapterPattern,
		ExtraRegex:       ExtraPattern,
		IntroRegex:       IntroPattern,
		IntroPrefixes: []string{
			"书籍简介：",
			"书籍简介:",
			"内容简介：",
			"内容简介:",
			"简介：",
			"简介:",
		},
		SpecialChapterTitles: []string{
			"楔子",
			"卷首语",
			"序",
			"引子",
			"序言",
			"完本感言",
			"楔子语",
		},
		IgnoredLinePatterns: []string{
			`^第[一二三四五六七八九十百零0-9]+(卷|部|集)接近尾声.*$`,
			`^第[一二三四五六七八九十百零0-9]+(卷|部|集)[:：].*[，,；;：:].*[。！？?!~～]\s*$`,
		},
		IgnoredLineContains: append([]string(nil), defaultAuthorNoteContains...),
	}
}

type loadedRuleConfig struct {
	Path            string
	SelectedChannel string
	Config          RuleConfig
}

func loadAutoRuleConfigs(channel string) ([]loadedRuleConfig, error) {
	paths := resolveAutoRuleConfigPaths()
	loaded := make([]loadedRuleConfig, 0, len(paths))

	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("检查规则配置文件失败 %s: %w", path, err)
		}
		if info.IsDir() {
			continue
		}

		cfg, selectedChannel, err := loadRuleConfig(path, channel)
		if err != nil {
			return nil, fmt.Errorf("加载自动规则配置失败 %s: %w", path, err)
		}
		loaded = append(loaded, loadedRuleConfig{
			Path:            path,
			SelectedChannel: selectedChannel,
			Config:          cfg,
		})
	}
	return loaded, nil
}

// ListRuleConfigSummaries 列出显式配置文件或自动发现到的规则文件及其可用渠道。
func ListRuleConfigSummaries(explicitPath string) ([]RuleConfigSummary, error) {
	paths := make([]string, 0, 4)
	if strings.TrimSpace(explicitPath) != "" {
		paths = append(paths, explicitPath)
	} else {
		paths = append(paths, resolveAutoRuleConfigPaths()...)
	}
	paths = dedupeStrings(paths)

	summaries := make([]RuleConfigSummary, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("检查规则配置文件失败 %s: %w", path, err)
		}
		if info.IsDir() {
			continue
		}

		fileCfg, err := loadRuleFileConfig(path)
		if err != nil {
			return nil, err
		}

		channels := make([]string, 0, len(fileCfg.Channels))
		for name := range fileCfg.Channels {
			channels = append(channels, name)
		}
		sort.Strings(channels)

		summaries = append(summaries, RuleConfigSummary{
			Path:           path,
			DefaultChannel: normalizeResolvedDefaultChannel(fileCfg),
			Channels:       channels,
			ChannelDetails: summarizeRuleChannels(fileCfg.Channels),
		})
	}
	return summaries, nil
}

// ResolveEffectiveRuleConfigSummary 返回最终生效的完整规则配置。
// 如果指定 explicitPath，则只解析该文件；否则按运行时自动发现顺序合并规则文件。
func ResolveEffectiveRuleConfigSummary(explicitPath string, requestedChannel string) (ResolvedRuleConfigSummary, error) {
	summary := ResolvedRuleConfigSummary{
		Config: defaultRuleConfig(),
	}

	if strings.TrimSpace(explicitPath) != "" {
		cfg, selectedChannel, err := loadRuleConfig(explicitPath, requestedChannel)
		if err != nil {
			return summary, err
		}
		var mergeErr error
		summary.Config, mergeErr = applyRulePresets(summary.Config, cfg.ExtendsPresets)
		if mergeErr != nil {
			return summary, mergeErr
		}
		summary.Config = mergeRuleConfig(summary.Config, cfg)
		summary.Sources = append(summary.Sources, ResolvedRuleSource{
			Path:            explicitPath,
			SelectedChannel: selectedChannel,
		})
		return summary, nil
	}

	autoConfigs, err := loadAutoRuleConfigs(requestedChannel)
	if err != nil {
		return summary, err
	}

	for _, loaded := range autoConfigs {
		summary.Config, err = applyRulePresets(summary.Config, loaded.Config.ExtendsPresets)
		if err != nil {
			return summary, fmt.Errorf("应用规则预设失败 %s: %w", loaded.Path, err)
		}
		summary.Config = mergeRuleConfig(summary.Config, loaded.Config)
		summary.Sources = append(summary.Sources, ResolvedRuleSource{
			Path:            loaded.Path,
			SelectedChannel: loaded.SelectedChannel,
		})
	}
	return summary, nil
}

func summarizeRuleChannels(channels map[string]RuleConfig) map[string]RuleChannelSummary {
	if len(channels) == 0 {
		return nil
	}

	summaries := make(map[string]RuleChannelSummary, len(channels))
	for name, cfg := range channels {
		summaries[name] = RuleChannelSummary{
			ExtendsPresets: append([]string(nil), cfg.ExtendsPresets...),
			DefinedFields:  summarizeRuleConfigFields(cfg),
		}
	}
	return summaries
}

func summarizeRuleConfigFields(cfg RuleConfig) []string {
	fields := make([]string, 0, 10)
	if strings.TrimSpace(cfg.TitleRegex) != "" {
		fields = append(fields, "title_regex")
	}
	if strings.TrimSpace(cfg.TitleAuthorRegex) != "" {
		fields = append(fields, "title_author_regex")
	}
	if strings.TrimSpace(cfg.AuthorRegex) != "" {
		fields = append(fields, "author_regex")
	}
	if strings.TrimSpace(cfg.VolumeRegex) != "" {
		fields = append(fields, "volume_regex")
	}
	if strings.TrimSpace(cfg.ChapterRegex) != "" {
		fields = append(fields, "chapter_regex")
	}
	if strings.TrimSpace(cfg.ExtraRegex) != "" {
		fields = append(fields, "extra_regex")
	}
	if strings.TrimSpace(cfg.IntroRegex) != "" {
		fields = append(fields, "intro_regex")
	}
	if len(cfg.IntroPrefixes) > 0 {
		fields = append(fields, "intro_prefixes")
	}
	if len(cfg.SpecialChapterTitles) > 0 {
		fields = append(fields, "special_chapter_titles")
	}
	if len(cfg.IgnoredLinePatterns) > 0 {
		fields = append(fields, "ignored_line_patterns")
	}
	if len(cfg.IgnoredLineContains) > 0 {
		fields = append(fields, "ignored_line_contains")
	}
	return fields
}

func mergeLoadedRuleConfigs(base RuleConfig, configs []loadedRuleConfig) (RuleConfig, error) {
	for _, loaded := range configs {
		var err error
		base, err = applyRulePresets(base, loaded.Config.ExtendsPresets)
		if err != nil {
			return base, fmt.Errorf("应用自动规则配置预设失败 %s: %w", loaded.Path, err)
		}
		base = mergeRuleConfig(base, loaded.Config)
	}
	return base, nil
}

func resolveAutoRuleConfigPaths() []string {
	paths := make([]string, 0, 4)

	if configDir, err := userConfigDirFunc(); err == nil && strings.TrimSpace(configDir) != "" {
		paths = append(paths, filepath.Join(configDir, defaultConfigDirName, defaultUserRuleConfigName))
	}

	if executablePath, err := executablePathFunc(); err == nil && strings.TrimSpace(executablePath) != "" {
		execDir := filepath.Dir(executablePath)
		execBase := strings.TrimSuffix(filepath.Base(executablePath), filepath.Ext(filepath.Base(executablePath)))
		paths = append(paths,
			filepath.Join(execDir, defaultProgramRuleConfigName),
			filepath.Join(execDir, execBase+".rules.toml"),
			filepath.Join(execDir, defaultConfigDirName+".rules.toml"),
		)
	}

	return dedupeStrings(paths)
}

// loadRuleConfig 从 TOML 配置文件中读取规则覆盖项，并根据渠道选择最终配置。
func loadRuleConfig(path string, requestedChannel string) (RuleConfig, string, error) {
	fileCfg, err := loadRuleFileConfig(path)
	if err != nil {
		return RuleConfig{}, "", err
	}
	resolvedConfig, selectedChannel, err := resolveRuleFileConfig(fileCfg, requestedChannel)
	if err != nil {
		return RuleConfig{}, "", err
	}
	return resolvedConfig, selectedChannel, nil
}

func loadRuleFileConfig(path string) (RuleFileConfig, error) {
	var fileCfg RuleFileConfig

	content, err := os.ReadFile(path)
	if err != nil {
		return fileCfg, fmt.Errorf("读取规则配置文件失败: %w", err)
	}

	meta, err := toml.Decode(string(content), &fileCfg)
	if err != nil {
		return fileCfg, fmt.Errorf("解析 TOML 规则配置文件失败: %w", err)
	}

	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		names := make([]string, 0, len(undecoded))
		for _, item := range undecoded {
			names = append(names, item.String())
		}
		return fileCfg, fmt.Errorf("规则配置文件包含未知字段: %s", strings.Join(names, ", "))
	}
	return fileCfg, nil
}

func resolveRuleFileConfig(fileCfg RuleFileConfig, requestedChannel string) (RuleConfig, string, error) {
	resolved := fileCfg.RuleConfig
	selectedChannel := strings.TrimSpace(strings.ToLower(requestedChannel))

	if selectedChannel == "" {
		selectedChannel = normalizeResolvedDefaultChannel(fileCfg)
	}
	if selectedChannel == "" {
		return resolved, "", nil
	}

	channelCfg, ok := findRuleChannelConfig(fileCfg.Channels, selectedChannel)
	if !ok {
		return RuleConfig{}, "", fmt.Errorf("规则文件中不存在渠道 %q", selectedChannel)
	}

	resolved.ExtendsPresets = appendUniqueStrings(resolved.ExtendsPresets, channelCfg.ExtendsPresets)
	channelCfg.ExtendsPresets = nil
	resolved = mergeRuleConfig(resolved, channelCfg)
	return resolved, selectedChannel, nil
}

func normalizeResolvedDefaultChannel(fileCfg RuleFileConfig) string {
	selectedChannel := strings.TrimSpace(strings.ToLower(fileCfg.DefaultChannel))
	if selectedChannel != "" {
		return selectedChannel
	}
	if _, ok := fileCfg.Channels["default"]; ok {
		return "default"
	}
	for key := range fileCfg.Channels {
		if strings.EqualFold(strings.TrimSpace(key), "default") {
			return key
		}
	}
	return ""
}

func findRuleChannelConfig(channels map[string]RuleConfig, name string) (RuleConfig, bool) {
	if len(channels) == 0 {
		return RuleConfig{}, false
	}
	if cfg, ok := channels[name]; ok {
		return cfg, true
	}
	for key, cfg := range channels {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return cfg, true
		}
	}
	return RuleConfig{}, false
}

// mergeRuleConfig 用配置文件内容覆盖内置规则。
// 字符串字段采用“非空覆盖”，切片字段采用“非空整体替换”。
func mergeRuleConfig(base RuleConfig, override RuleConfig) RuleConfig {
	if strings.TrimSpace(override.TitleRegex) != "" {
		base.TitleRegex = override.TitleRegex
	}
	if strings.TrimSpace(override.TitleAuthorRegex) != "" {
		base.TitleAuthorRegex = override.TitleAuthorRegex
	}
	if strings.TrimSpace(override.AuthorRegex) != "" {
		base.AuthorRegex = override.AuthorRegex
	}
	if strings.TrimSpace(override.VolumeRegex) != "" {
		base.VolumeRegex = override.VolumeRegex
	}
	if strings.TrimSpace(override.ChapterRegex) != "" {
		base.ChapterRegex = override.ChapterRegex
	}
	if strings.TrimSpace(override.ExtraRegex) != "" {
		base.ExtraRegex = override.ExtraRegex
	}
	if strings.TrimSpace(override.IntroRegex) != "" {
		base.IntroRegex = override.IntroRegex
	}
	if len(override.IntroPrefixes) > 0 {
		base.IntroPrefixes = append([]string(nil), override.IntroPrefixes...)
	}
	if len(override.SpecialChapterTitles) > 0 {
		base.SpecialChapterTitles = append([]string(nil), override.SpecialChapterTitles...)
	}
	if len(override.IgnoredLinePatterns) > 0 {
		base.IgnoredLinePatterns = append([]string(nil), override.IgnoredLinePatterns...)
	}
	if len(override.IgnoredLineContains) > 0 {
		base.IgnoredLineContains = append([]string(nil), override.IgnoredLineContains...)
	}
	return base
}

// applyRulePresets 将一个或多个命名预设扩展到基础规则中。
// 预设采用“追加扩展”的方式合并，尽量保留通用规则并补充来源特征。
func applyRulePresets(base RuleConfig, names []string) (RuleConfig, error) {
	for _, name := range names {
		preset, ok := builtinRulePresets[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return base, fmt.Errorf("未知规则预设: %s", name)
		}
		base = extendRuleConfig(base, preset.Config)
	}
	return base, nil
}

// DetectRulePresets 根据文本特征自动探测适合的规则预设。
// 该探测只用于推荐或自动应用命名预设，不会替代用户显式传入的规则。
func DetectRulePresets(text string) []DetectedRulePreset {
	lines := buildDetectionLines(text)
	results := make([]DetectedRulePreset, 0, len(builtinRulePresets))

	for name, preset := range builtinRulePresets {
		detected := detectSinglePreset(name, preset.Detector, lines)
		if detected == nil {
			continue
		}
		results = append(results, *detected)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Name < results[j].Name
		}
		return results[i].Score > results[j].Score
	})
	return results
}

func detectSinglePreset(name string, detector PresetDetector, lines []string) *DetectedRulePreset {
	if len(lines) == 0 {
		return nil
	}

	score := 0
	reasons := make([]string, 0, len(detector.Contains)+len(detector.Regexps))
	seenReasons := make(map[string]struct{}, len(detector.Contains)+len(detector.Regexps))
	corpus := strings.Join(lines, "\n")

	for _, keyword := range detector.Contains {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" || !strings.Contains(corpus, keyword) {
			continue
		}
		if _, ok := seenReasons[keyword]; ok {
			continue
		}
		seenReasons[keyword] = struct{}{}
		reasons = append(reasons, keyword)
		score++
	}

	for _, pattern := range detector.Regexps {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		regex, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		matched := false
		for _, line := range lines {
			if regex.MatchString(line) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if _, ok := seenReasons[pattern]; ok {
			continue
		}
		seenReasons[pattern] = struct{}{}
		reasons = append(reasons, pattern)
		score += 2
	}

	minimumScore := detector.MinimumScore
	if minimumScore <= 0 {
		minimumScore = 1
	}
	if score < minimumScore {
		return nil
	}
	return &DetectedRulePreset{
		Name:    name,
		Score:   score,
		Reasons: reasons,
	}
}

func buildDetectionLines(text string) []string {
	const maxLines = 400
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, min(len(lines), maxLines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
		if len(filtered) >= maxLines {
			break
		}
	}
	return filtered
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extendRuleConfig 用于预设扩展场景。
// 字符串字段允许覆盖，切片字段采用追加去重，适合在默认规则上叠加来源差异。
func extendRuleConfig(base RuleConfig, extension RuleConfig) RuleConfig {
	if strings.TrimSpace(extension.TitleRegex) != "" {
		base.TitleRegex = extension.TitleRegex
	}
	if strings.TrimSpace(extension.TitleAuthorRegex) != "" {
		base.TitleAuthorRegex = extension.TitleAuthorRegex
	}
	if strings.TrimSpace(extension.AuthorRegex) != "" {
		base.AuthorRegex = extension.AuthorRegex
	}
	if strings.TrimSpace(extension.VolumeRegex) != "" {
		base.VolumeRegex = extension.VolumeRegex
	}
	if strings.TrimSpace(extension.ChapterRegex) != "" {
		base.ChapterRegex = extension.ChapterRegex
	}
	if strings.TrimSpace(extension.ExtraRegex) != "" {
		base.ExtraRegex = extension.ExtraRegex
	}
	if strings.TrimSpace(extension.IntroRegex) != "" {
		base.IntroRegex = extension.IntroRegex
	}

	base.IntroPrefixes = appendUniqueStrings(base.IntroPrefixes, extension.IntroPrefixes)
	base.SpecialChapterTitles = appendUniqueStrings(base.SpecialChapterTitles, extension.SpecialChapterTitles)
	base.IgnoredLinePatterns = appendUniqueStrings(base.IgnoredLinePatterns, extension.IgnoredLinePatterns)
	base.IgnoredLineContains = appendUniqueStrings(base.IgnoredLineContains, extension.IgnoredLineContains)
	return base
}

func appendUniqueStrings(base []string, additions []string) []string {
	result := append([]string(nil), base...)
	seen := make(map[string]struct{}, len(result))

	for _, item := range result {
		normalized := strings.TrimSpace(item)
		if normalized != "" {
			seen[normalized] = struct{}{}
		}
	}

	for _, item := range additions {
		normalized := strings.TrimSpace(item)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func dedupeStrings(items []string) []string {
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))

	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

// compileRuleConfig 将字符串规则编译为可直接执行的解析规则。
func compileRuleConfig(cfg RuleConfig) (*ParseRules, error) {
	titleRegex, err := regexp.Compile(cfg.TitleRegex)
	if err != nil {
		return nil, fmt.Errorf("书名正则无效: %w", err)
	}
	titleAuthorRegex, err := regexp.Compile(cfg.TitleAuthorRegex)
	if err != nil {
		return nil, fmt.Errorf("标题作者联合正则无效: %w", err)
	}
	authorRegex, err := regexp.Compile(cfg.AuthorRegex)
	if err != nil {
		return nil, fmt.Errorf("作者正则无效: %w", err)
	}
	volumeRegex, err := regexp.Compile(cfg.VolumeRegex)
	if err != nil {
		return nil, fmt.Errorf("卷正则无效: %w", err)
	}
	chapterRegex, err := regexp.Compile(cfg.ChapterRegex)
	if err != nil {
		return nil, fmt.Errorf("章节正则无效: %w", err)
	}
	extraRegex, err := regexp.Compile(cfg.ExtraRegex)
	if err != nil {
		return nil, fmt.Errorf("番外正则无效: %w", err)
	}
	introRegex, err := regexp.Compile(cfg.IntroRegex)
	if err != nil {
		return nil, fmt.Errorf("简介正则无效: %w", err)
	}

	ignoredLineRegexps := make([]*regexp.Regexp, 0, len(cfg.IgnoredLinePatterns))
	for _, pattern := range cfg.IgnoredLinePatterns {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("忽略行正则无效 %q: %w", pattern, err)
		}
		ignoredLineRegexps = append(ignoredLineRegexps, compiled)
	}

	specialChapterSet := make(map[string]struct{}, len(cfg.SpecialChapterTitles))
	for _, title := range cfg.SpecialChapterTitles {
		title = strings.TrimSpace(title)
		if title != "" {
			specialChapterSet[title] = struct{}{}
		}
	}

	introPrefixes := make([]string, 0, len(cfg.IntroPrefixes))
	for _, prefix := range cfg.IntroPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" {
			introPrefixes = append(introPrefixes, prefix)
		}
	}

	ignoredLineContains := make([]string, 0, len(cfg.IgnoredLineContains))
	for _, keyword := range cfg.IgnoredLineContains {
		keyword = strings.TrimSpace(keyword)
		if keyword != "" {
			ignoredLineContains = append(ignoredLineContains, keyword)
		}
	}

	return &ParseRules{
		TitleRegex:          titleRegex,
		TitleAuthorRegex:    titleAuthorRegex,
		AuthorRegex:         authorRegex,
		VolumeRegex:         volumeRegex,
		ChapterRegex:        chapterRegex,
		ExtraRegex:          extraRegex,
		IntroRegex:          introRegex,
		IntroPrefixes:       introPrefixes,
		SpecialChapterSet:   specialChapterSet,
		IgnoredLineRegexps:  ignoredLineRegexps,
		IgnoredLineContains: ignoredLineContains,
	}, nil
}

// ShouldIgnoreLine 判断一行文本是否应被当作作者说明、更新提示等噪音行跳过。
func (r *ParseRules) ShouldIgnoreLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}

	for _, regex := range r.IgnoredLineRegexps {
		if regex.MatchString(trimmed) {
			return true
		}
	}

	for _, keyword := range r.IgnoredLineContains {
		if strings.Contains(trimmed, keyword) {
			return true
		}
	}
	return false
}

// IsSpecialChapterTitle 判断一行文本是否属于特殊章节标题。
func (r *ParseRules) IsSpecialChapterTitle(line string) bool {
	trimmed := strings.TrimSpace(line)
	if _, ok := r.SpecialChapterSet[trimmed]; ok {
		return true
	}
	return strings.HasPrefix(trimmed, "简介") || strings.HasPrefix(trimmed, "内容简介")
}

// IsStructuralLine 判断文本行是否已经进入卷章正文结构。
func (r *ParseRules) IsStructuralLine(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	return r.ShouldIgnoreLine(line) ||
		(r.VolumeRegex != nil && r.VolumeRegex.MatchString(line)) ||
		(r.ChapterRegex != nil && r.ChapterRegex.MatchString(line)) ||
		(r.ExtraRegex != nil && r.ExtraRegex.MatchString(line)) ||
		r.IsSpecialChapterTitle(line)
}

// ParseInlineTitleAndAuthor 解析“书名 作者：作者名”这种合并写法。
func (r *ParseRules) ParseInlineTitleAndAuthor(line string) (string, string, bool) {
	matches := r.TitleAuthorRegex.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 3 {
		return "", "", false
	}

	title := strings.TrimSpace(matches[1])
	author := strings.TrimSpace(matches[2])
	if title == "" || author == "" {
		return "", "", false
	}
	return title, author, true
}

// ParseTitle 使用标题正则解析书名。
// 如果正则带有捕获组，则优先返回第一个捕获组；否则返回整行文本。
func (r *ParseRules) ParseTitle(line string) (string, bool) {
	return parseFieldByRegex(line, r.TitleRegex)
}

// ParseAuthor 使用作者正则解析作者名。
// 如果正则带有捕获组，则优先返回第一个捕获组；否则返回整行文本。
func (r *ParseRules) ParseAuthor(line string) (string, bool) {
	return parseFieldByRegex(line, r.AuthorRegex)
}

// ParsePrefixedIntro 解析“书籍简介：xxx”这种前缀式简介。
func (r *ParseRules) ParsePrefixedIntro(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range r.IntroPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix)), true
		}
	}
	return "", false
}

func parseFieldByRegex(line string, regex *regexp.Regexp) (string, bool) {
	if regex == nil {
		return "", false
	}

	matches := regex.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) == 0 {
		return "", false
	}
	if len(matches) > 1 {
		value := strings.TrimSpace(matches[1])
		if value == "" {
			return "", false
		}
		return value, true
	}

	value := strings.TrimSpace(matches[0])
	if value == "" {
		return "", false
	}
	return value, true
}
