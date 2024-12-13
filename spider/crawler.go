package spider

import (
	"context"
	"fmt"
	"log"
	"time"
)

type CrawlerParams struct {
	// 小说目录url
	ChapterURL string `json:"chapter_url"`
	// 使用的抓取规则
	RuleName string `json:"rule_name"`
	// 抓取的开始章节
	ChapterStart int64 `json:"chapter_start"`
	// 抓取的结束章节
	ChapterEnd int64 `json:"chapter_end"`
}

func Crawler(ctx context.Context, param *CrawlerParams) (*Book, error) {
	ruleName := param.RuleName
	bookRule, ok := DefaultRule.Rule(ruleName)
	if !ok {
		return nil, fmt.Errorf("未找到小说抓取规则:%s", ruleName)
	}
	bookSpider, ok := DefaultContainer.Spider(bookRule.RuleName)
	if bookSpider == nil {
		return nil, fmt.Errorf("未找到支持该规则的抓取程序:%s", ruleName)
	}
	var book Book

	start := time.Now()
	log.Printf("正在抓取小说 -> [小说源: %s] [使用规则:%s] [抓取实现:%s]", param.ChapterURL, ruleName, bookSpider.Name())
	metadata, mErr := bookSpider.CrawlMetadata(ctx, param.ChapterURL, &bookRule.Metadata)
	if mErr != nil {
		return nil, fmt.Errorf("抓取小说元数据失败:%s - %w", ruleName, mErr)
	}

	book.Name = metadata.Name
	book.Intro = metadata.Intro
	book.Author = metadata.Author
	book.Cover = metadata.Cover
	book.URL = metadata.URL
	book.Category = metadata.Category
	log.Printf("元数据抓取完成：[小说名称:%s] [作者:%s] %.2fs", book.Name, book.Author, time.Now().Sub(start).Seconds())
	log.Printf("开始抓取小说目录:[目录地址:%s]", param.ChapterURL)
	start = time.Now()
	catalogs, cErr := bookSpider.CrawlCatalog(ctx, param.ChapterURL, &bookRule.Chapter)
	if cErr != nil {
		return nil, cErr
	}
	log.Printf("小说目录抓取完成：[章节数:%d]  %.2fs", len(catalogs), time.Now().Sub(start).Seconds())
	var vol Volume
	for i, catalog := range catalogs {
		start = time.Now()
		log.Printf("开始处理章节:%s - %s", catalog.Title, catalog.URL)
		if param.ChapterStart > 0 && param.ChapterStart > int64(i) {
			log.Printf("跳过章节:%s", catalog.Title)
			continue
		}
		if param.ChapterEnd > 0 && param.ChapterEnd < int64(i) {
			log.Println("跳过剩余章节")
			break
		}
		content, ctErr := bookSpider.CrawlContent(ctx, catalog.URL, &bookRule.Content)
		if ctErr != nil {
			if bookRule.Content.SkipErr {
				log.Printf("抓取章节失败 -> [章节名称:%s] [章节地址:%s]", catalog.Title, catalog.URL)
				content = "小说内容抓取错误：" + ctErr.Error()
			} else {
				return nil, fmt.Errorf("抓取章节失败 -> [章节名称:%s] [章节地址:%s] %w", catalog.Title, catalog.URL, ctErr)
			}
		}
		vol.Chapters = append(vol.Chapters, Chapter{
			Title:   catalog.Title,
			Content: content,
			URL:     catalog.URL,
		})
		log.Printf("章节处理完成：[章节名称:%s]  %.2fs", catalog.Title, time.Now().Sub(start).Seconds())
	}
	book.Volumes = append(book.Volumes, vol)

	return &book, nil
}
