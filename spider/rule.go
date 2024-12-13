package spider

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/lifei6671/gotexttoepub/internal/util"
)

var DefaultRule *BookRuleSet

type NovelSource struct {
	Includes []string `toml:"includes"`
}

type Selector struct {
	Selector string `toml:"selector"`
	Index    int    `toml:"index"`
	Attr     string `toml:"attr"`
}

// BookRule 小说抓取规则
type BookRule struct {
	RuleName string `toml:"rule_name"`
	//小说元数据
	Metadata MetadataRule `toml:"metadata"`
	//小说章节
	Chapter ChapterRule `toml:"chapter"`
	//小说内容
	Content ContentRule `toml:"content"`
}

// MetadataRule 小说元数据抓取规则
type MetadataRule struct {
	// 小说名称抓取规则
	NameRegexp Selector `toml:"name_regexp"`
	// 小说作者
	AuthorRegexp Selector `toml:"author_regexp"`
	// 小说简介
	IntroRegexp Selector `toml:"intro_regexp"`
	// 小说分类
	CategoryRegexp Selector `toml:"category_regexp"`
	// 小说封面
	CoverRegexp Selector `toml:"cover_regexp"`
}

// ChapterRule 小说章节抓取规则
type ChapterRule struct {
	// 章节是否开启了分页
	IsPagination bool `toml:"is_pagination"`
	// 如果开启了分页分页抓取规则
	PaginationRegexp []Selector `toml:"pagination_regexp"`
	// 章节列表抓取规则
	CatalogRegexp []Selector `toml:"catalog_regexp"`
}

// ContentRule 小说内容抓取规则
type ContentRule struct {
	// 小说内容是否开启了分页
	IsPagination bool `toml:"is_pagination"`
	// 如果开启了分页，分页的抓取规则
	PaginationRegexp struct {
		SelectorGroup []Selector `toml:"selector_group"`
		EndText       string     `toml:"end_text"`
	} `toml:"pagination_regexp"`
	// 内容的抓取规则
	ContentRegexp []Selector `toml:"content_regexp"`
	SkipErr       bool       `toml:"skip_err"`
	// 需要过滤的文本
	FilterText []string `toml:"filter_text"`
	// 需要过滤的html标签名称
	FilterHTML []string `toml:"filter_html"`
	WaitTime   int      `toml:"wait_time"`
}

// BookRuleSet 小说抓取规则集合
type BookRuleSet struct {
	rules map[string]BookRule
}

// Rule 获取指定名称的规则
func (b *BookRuleSet) Rule(name string) (*BookRule, bool) {
	if b.rules == nil {
		return nil, false
	}
	if rule, ok := b.rules[name]; ok {
		return &rule, true
	}
	return nil, false
}

// LoadRule 解析配置
func LoadRule(source string) (*BookRuleSet, error) {
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %s: %w", source, err)
	}
	var v NovelSource
	_, dErr := toml.DecodeFile(sourcePath, &v)
	if dErr != nil {
		return nil, fmt.Errorf("decode file failed %s:%w", sourcePath, dErr)
	}
	baseDir := filepath.Dir(sourcePath)
	set := BookRuleSet{
		rules: map[string]BookRule{},
	}

	for _, p := range v.Includes {
		filename, pErr := util.ResolvePath(baseDir, p)
		if pErr != nil {
			return nil, fmt.Errorf("failed to resolve path %s: %w", source, err)
		}
		var obj BookRule
		_, rErr := toml.DecodeFile(filename, &obj)
		if rErr != nil {
			return nil, fmt.Errorf("read config file err: %s - %w", p, rErr)
		}
		set.rules[obj.RuleName] = obj
	}
	return &set, nil
}

func InitRule(source string) error {
	var err error
	DefaultRule, err = LoadRule(source)
	if err != nil {
		log.Println(err)
	}
	return err
}

func init() {
	err := InitRule("./conf/source.toml")
	if err != nil {
		panic(err)
	}
}
