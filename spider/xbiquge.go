package spider

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"

	"github.com/lifei6671/gotexttoepub/internal/util"
)

type xBiQuGe struct {
}

func (x *xBiQuGe) CrawlMetadata(ctx context.Context, urlStr string, rule *MetadataRule) (*Metadata, error) {
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
				return node.AttrOr(selector.Attr, "")
			} else {
				return node.Text()
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

func (x *xBiQuGe) CrawlCatalog(ctx context.Context, urlStr string, rule *ChapterRule) ([]*Catalog, error) {
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

		for i, selection := range doc.Find(rule.CatalogRegexp.Selector).EachIter() {
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
			pager := doc.Find(rule.PaginationRegexp.Selector)

			if rule.PaginationRegexp.Attr != "" {
				nextURLStr = pager.AttrOr(rule.PaginationRegexp.Attr, "")
			} else {
				nextURLStr = pager.AttrOr("href", "")
			}
		}
		return catalogList, nextURLStr, nil
	}
	var catalogList []*Catalog
	for {
		list, urlStr, err := catalogClientFn(ctx, urlStr)
		if err != nil {
			return nil, err
		}
		catalogList = append(catalogList, list...)
		if urlStr == "" {
			break
		}
	}

	return catalogList, nil
}

func (x *xBiQuGe) CrawlContent(ctx context.Context, urlStr string, rule *ContentRule) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (x *xBiQuGe) Name() string {
	return "香书小说"
}

func NewXBiQuGe() Spider {
	return &xBiQuGe{}
}
