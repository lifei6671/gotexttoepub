package spider

import (
	"context"

	"github.com/PuerkitoBio/goquery"
)

var DefaultHeader = map[string]string{
	"Accept":          "text/html, application/xhtml+xml",
	"Accept-Encoding": "gzip, deflate",
	"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6,mt;q=0.5,ru;q=0.4,de;q=0.3",
	"User-Agent":      "Mozilla/5.0 (iPhone;CPU iPhone OS 9_1 like Mac OS X) AppleWebKit/601.1.46 (KHTML, like Gecko)Version/9.0 Mobile/13B143 Safari/601.1 (compatible; Baiduspider-render/2.0;+http://www.baidu.com/search/spider.html)",
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

// Metadata 小说元数据
type Metadata struct {
	// 标题
	Name string `json:"name"`
	// 作者
	Author string `json:"author"`
	// 小说分类
	Category string `json:"category"`
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

// ExecSelector 批量执行选择器
func ExecSelector(doc *goquery.Document, selectors []Selector) *goquery.Selection {
	selection := doc.Find("body")
	for _, selector := range selectors {
		selection = selection.Find(selector.Selector)
		if selector.Index >= 0 {
			selection = selection.Eq(selector.Index)
		}
	}
	return selection
}
