package spider

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/lifei6671/gotexttoepub/internal/util"
)

type NovelSource struct {
	Includes []string `toml:"includes"`
}

// BookRule 小说抓取规则
type BookRule struct {
	//小说元数据
	Metadata MetadataRule `toml:"metadata"`
	//小说章节
	Chapter ChapterRule `toml:"chapter"`
	//小说内容
	Content ContentRule `toml:"content"`
}

// MetadataRule 小说元数据抓取规则
type MetadataRule struct {
	// 小说规则名称
	RuleName string `toml:"rule_name"`
	// 小说链接
	URL string `toml:"url"`
	// 小说名称抓取规则
	NameRegexp string `toml:"name_regexp"`
	// 小说作者
	AuthorRegexp string `toml:"author_regexp"`
	// 小说简介
	IntroRegexp string `toml:"intro_regexp"`
	// 小说分类
	CategoryRegexp string `toml:"category_regexp"`
	// 小说封面
	CoverRegexp string `toml:"cover_regexp"`
}

// ChapterRule 小说章节抓取规则
type ChapterRule struct {
	// 章节是否开启了分页
	IsPagination bool `toml:"is_pagination"`
	// 如果开启了分页分页抓取规则
	PaginationRegexp string `json:"pagination_regexp"`
	// 章节列表抓取规则
	CatalogRegexp string `toml:"catalog_regexp"`
}

// ContentRule 小说内容抓取规则
type ContentRule struct {
	// 小说内容是否开启了分页
	IsPagination bool `toml:"is_pagination"`
	// 如果开启了分页，分页的抓取规则
	PaginationRegexp string `json:"pagination_regexp"`
	// 内容的抓取规则
	ContentRegexp string `toml:"content_regexp"`
	// 需要过滤的文本
	FilterText []string `toml:"filter_text"`
	// 需要过滤的html标签名称
	FilterHTML []string `toml:"filter_html"`
}
type Book struct {
	// 标题
	Name string `json:"name"`
	// 作者
	Author string `json:"author"`
	// 小说原地址
	URL string `json:"url"`
	//使用的抓取规则名称
	RuleName string `json:"rule_name"`
	// 解析后的章节
	Volumes []Volume `json:"volumes"`
	// 封面
	Cover string `json:"cover"`
	// 语言
	Lang string `json:"lang"`
	// 简介
	Intro string `json:"intro"`
	// 出版社
	Publisher string `json:"publisher"`
	// 出版日期
	PublishDate string `json:"publish_date"`
}

// Volume 卷的结构
type Volume struct {
	// 卷标题
	Title string `json:"title"`
	// 章节列表
	Chapters []Chapter `json:"chapters"`
}

// Chapter 章节的结构
type Chapter struct {
	// 章节标题
	Title string `json:"title"`
	// 章节内容
	Content string `json:"content"`
	// 小说源地址
	URL string `json:"url"`
}

// LoadRule 解析配置
func LoadRule(source string) ([]*BookRule, error) {
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

	var rules []*BookRule
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
		rules = append(rules, &obj)
	}
	return rules, nil
}

// Metadata 小说元数据
type Metadata struct {
	// 标题
	Name string `json:"name"`
	// 作者
	Author string `json:"author"`
	// 小说原地址
	URL string `json:"url"`
	// 封面
	Cover string `json:"cover"`
	// 语言
	Lang string `json:"lang"`
	// 简介
	Intro string `json:"intro"`
	// 出版社
	Publisher string `json:"publisher"`
	// 出版日期
	PublishDate string `json:"publish_date"`
}

// Catalog 目录数据
type Catalog struct {
	// 卷标题
	VolTitle string `json:"vol_title"`
	// 目录标题
	Title string `json:"title"`
	// 目录内容地址
	URL string `json:"url"`
	//排序索引
	Index int `json:"index"`
}

// Spider 小说抓取接口
type Spider interface {
	// CrawlMetadata 抓取小说元数据
	CrawlMetadata(ctx context.Context, urlStr string, rule *MetadataRule) (*Metadata, error)
	// CrawlCatalog 抓取小说目录
	CrawlCatalog(ctx context.Context, urlStr string, rule *ChapterRule) ([]*Catalog, error)
	// CrawlContent 住区指定小说内容
	CrawlContent(ctx context.Context, urlStr string, rule *ContentRule) (string, error)
	Name() string
}
