package spider

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"

	"github.com/lifei6671/gotexttoepub/internal/util"
)

type common struct {
}

func (x *common) CrawlMetadata(ctx context.Context, urlStr string, rule *MetadataRule) (*Metadata, error) {
	client := resty.New()
	if util.IsInTest() {
		httpmock.ActivateNonDefault(client.GetClient())
	}
	client.AddRetryCondition(func(r *resty.Response, err error) bool {
		return err != nil || r.StatusCode() == http.StatusTooManyRequests
	})
	resp, err := client.
		SetRetryCount(3).
		SetRetryWaitTime(time.Second * 5).
		SetRetryMaxWaitTime(time.Second * 20).
		SetHeaders(DefaultHeader).
		R().
		SetContext(ctx).
		Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("request url failed: %s %w", urlStr, err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("request url failed: %s", resp.Status())
	}

	doc, nErr := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body()))
	if nErr != nil {
		return nil, fmt.Errorf("parse html failed:%w", nErr)
	}
	findFn := func(selector Selector) string {
		if selector.Selector != "" {
			node := doc.Find(selector.Selector)
			if selector.Attr != "" {
				return strings.TrimSpace(node.AttrOr(selector.Attr, ""))
			} else {
				return strings.TrimSpace(node.Text())
			}
		}
		return ""
	}
	metadata := &Metadata{
		Name:        findFn(rule.NameRegexp),
		Author:      findFn(rule.AuthorRegexp),
		Category:    findFn(rule.CategoryRegexp),
		URL:         urlStr,
		Cover:       findFn(rule.CoverRegexp),
		Lang:        "zh_CN",
		Intro:       findFn(rule.IntroRegexp),
		Publisher:   "",
		PublishDate: "",
	}
	return metadata, nil
}

func (x *common) CrawlCatalog(ctx context.Context, urlStr string, rule *ChapterRule) ([]*Catalog, error) {
	// 定义目录抓取函数，方便后续分页抓取
	catalogClientFn := func(ctx context.Context, urlStr string) ([]*Catalog, string, error) {
		client := resty.New()
		if util.IsInTest() {
			httpmock.ActivateNonDefault(client.GetClient())
		}
		client.AddRetryCondition(func(r *resty.Response, err error) bool {
			return err != nil || r.StatusCode() == http.StatusTooManyRequests
		})
		resp, err := client.
			SetRetryCount(3).
			SetRetryWaitTime(time.Second * 5).
			SetRetryMaxWaitTime(time.Second * 20).
			SetHeaders(DefaultHeader).
			R().
			SetContext(ctx).
			Get(urlStr)
		if err != nil {
			return nil, "", fmt.Errorf("request url failed: %s %w", urlStr, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, "", fmt.Errorf("request url failed: %s", resp.Status())
		}

		doc, nErr := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body()))
		if nErr != nil {
			return nil, "", fmt.Errorf("parse html failed:%w", nErr)
		}
		var catalogList []*Catalog
		for i, selection := range ExecSelector(doc, rule.CatalogRegexp).EachIter() {
			uStr, uErr := util.ResolveFullURL(urlStr, selection.AttrOr("href", ""))
			if uErr != nil {
				return nil, "", fmt.Errorf("parse catalog url failed:%s - %w", selection.Text(), uErr)
			}
			catalog := &Catalog{
				URL:   uStr,
				Title: selection.Text(),
				Index: i,
			}
			catalogList = append(catalogList, catalog)
		}
		nextURLStr := ""
		if rule.IsPagination {
			uStr := ExecSelector(doc, rule.PaginationRegexp).AttrOr("href", "")
			//将相对路径转换为绝对路径
			if fullURL, fErr := util.ResolveFullURL(urlStr, uStr); fErr == nil {
				nextURLStr = fullURL
			}
		}
		return catalogList, nextURLStr, nil
	}
	var catalogList []*Catalog
	nextURLStr := urlStr
	for {
		list, nextStr, err := catalogClientFn(ctx, nextURLStr)
		if err != nil {
			return nil, err
		}
		nextURLStr = nextStr
		catalogList = append(catalogList, list...)
		if nextURLStr == "" {
			break
		}
	}

	return catalogList, nil
}

func (x *common) CrawlContent(ctx context.Context, urlStr string, rule *ContentRule) (string, error) {
	nextStr := urlStr
	b := &strings.Builder{}
	var err error
	for {
		nextStr, err = x.parseContent(ctx, nextStr, b, rule)
		if err != nil {
			return "", fmt.Errorf("parse content err:%w", err)
		}
		if nextStr != "" && rule.WaitTime > 0 {
			time.Sleep(time.Microsecond * time.Duration(rule.WaitTime))
		}
		if nextStr == "" {
			return b.String(), nil
		}
	}
}

func (x *common) parseContent(ctx context.Context, urlStr string, b *strings.Builder, rule *ContentRule) (string, error) {
	client := resty.New()
	if util.IsInTest() {
		httpmock.ActivateNonDefault(client.GetClient())
	}
	client.AddRetryCondition(func(r *resty.Response, err error) bool {
		return err != nil || r.StatusCode() == http.StatusTooManyRequests
	})
	resp, err := client.
		SetRetryCount(3).
		SetRetryWaitTime(time.Second * 5).
		SetRetryMaxWaitTime(time.Second * 20).
		SetHeaders(DefaultHeader).
		R().
		SetContext(ctx).
		Get(urlStr)
	if err != nil {
		return "", fmt.Errorf("request url failed: %s %w", urlStr, err)
	}
	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("request url failed: %s", resp.Status())
	}

	doc, nErr := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body()))
	if nErr != nil {
		return "", fmt.Errorf("parse html failed:%w", nErr)
	}
	for _, selection := range ExecSelector(doc, rule.ContentRegexp).EachIter() {
		for _, filterHtml := range rule.FilterHTML {
			// 删除指定的标签
			_ = selection.RemoveFiltered(filterHtml)
		}
		s := strings.TrimSpace(selection.Text())
		if s != "" {
			for _, text := range rule.FilterText {
				s = strings.ReplaceAll(s, text, "")
			}
			_, _ = b.WriteString(s)
		}
	}

	nextURLStr := ""
	if rule.IsPagination {
		uStr := ExecSelector(doc, rule.PaginationRegexp).AttrOr("href", "")
		//将相对路径转换为绝对路径
		if fullURL, fErr := util.ResolveFullURL(urlStr, uStr); fErr == nil {
			nextURLStr = fullURL
		}
	}
	return nextURLStr, nil
}

func (x *common) Name() string {
	return "common"
}

func NewCommonSpider() Spider {
	return &common{}
}
