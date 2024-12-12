package spider

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jarcoal/httpmock"
	"github.com/smartystreets/goconvey/convey"
)

func TestCommon_CrawlMetadata(t *testing.T) {
	httpmock.Activate()
	t.Cleanup(httpmock.DeactivateAndReset)
	body, _ := os.ReadFile(filepath.Join("testdata", "biquge_catalog.html"))
	httpmock.RegisterResponder(http.MethodGet, "https://www.01xs.com/xiaoshuo/106642/",
		httpmock.NewBytesResponder(http.StatusOK, body))
	httpmock.RegisterResponder(http.MethodGet, "https://www.01xs.com/xiaoshuo/106643/",
		httpmock.NewBytesResponder(http.StatusForbidden, nil))

	httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244.html",
		NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_catalog.html")))

	convey.Convey("Common_CrawlMetadata", t, func() {
		ctx := context.TODO()
		rules, err := LoadRule("../conf/source.toml")
		convey.So(err, convey.ShouldBeNil)
		rule, ok := rules.Rule("www.01xs.com")
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(rule, convey.ShouldNotBeNil)
		convey.So(rule, convey.ShouldNotBeNil)
		zxrssRule, ok := rules.Rule("m.axrss.com")

		ins := NewCommonSpider()
		convey.Convey("Common_CrawlMetadata_OK", func() {
			metadata, err := ins.CrawlMetadata(ctx, "https://www.01xs.com/xiaoshuo/106642/", &rule.Metadata)
			convey.So(err, convey.ShouldBeNil)
			convey.So(metadata, convey.ShouldNotBeNil)
			convey.So(metadata.Name, convey.ShouldEqual, "梦回大明春")
			convey.So(metadata.Author, convey.ShouldEqual, "王梓钧")
			convey.So(metadata.Category, convey.ShouldEqual, "军史穿越")
			convey.So(strings.HasPrefix(metadata.Intro, "穿越到大明朝"), convey.ShouldBeTrue)
		})
		convey.Convey("Common_CrawlMetadata_axrss_OK", func() {
			metadata, err := ins.CrawlMetadata(ctx, "https://m.axrss.com/book/566244.html", &zxrssRule.Metadata)
			convey.So(err, convey.ShouldBeNil)
			convey.So(metadata, convey.ShouldNotBeNil)
			convey.So(metadata.Name, convey.ShouldEqual, "万历明君")
			convey.So(metadata.Author, convey.ShouldEqual, "鹤招")
			convey.So(metadata.Category, convey.ShouldEqual, "穿越小说")
			convey.So(strings.HasPrefix(metadata.Intro, "公元1572年"), convey.ShouldBeTrue)
		})
		convey.Convey("Common_CrawlMetadata_Err", func() {
			metadata, err := ins.CrawlMetadata(ctx, "https://www.01xs.com/xiaoshuo/106643/", &rule.Metadata)
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(metadata, convey.ShouldBeNil)
		})
	})
}

func TestCommon_CrawlCatalog(t *testing.T) {
	httpmock.Activate()
	t.Cleanup(httpmock.DeactivateAndReset)
	httpmock.RegisterResponder(http.MethodGet, "https://www.01xs.com/xiaoshuo/106642/",
		NewFileResponder(http.StatusOK, filepath.Join("testdata", "biquge_catalog.html")))
	httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244.html",
		NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_catalog.html")))
	httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244_2.html",
		NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_catalog_2.html")))
	httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244_3.html",
		NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_catalog_3.html")))
	convey.Convey("Common_CrawlCatalog", t, func() {
		ctx := context.TODO()
		rules, err := LoadRule("../conf/source.toml")
		convey.So(err, convey.ShouldBeNil)
		rule, ok := rules.Rule("www.01xs.com")
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(rule, convey.ShouldNotBeNil)

		ins := NewCommonSpider()

		convey.Convey("Common_CrawlCatalog_OK", func() {
			catalog, err := ins.CrawlCatalog(ctx, "https://www.01xs.com/xiaoshuo/106642/", &rule.Chapter)
			convey.So(err, convey.ShouldBeNil)
			convey.So(catalog, convey.ShouldNotBeNil)
			convey.So(len(catalog), convey.ShouldEqual, 799)
		})

		convey.Convey("Common_CrawlCatalog_Page_OK", func() {
			zxrssRule, ok := rules.Rule("m.axrss.com")
			convey.So(ok, convey.ShouldBeTrue)
			catalog, err := ins.CrawlCatalog(ctx, "https://m.axrss.com/book/566244.html", &zxrssRule.Chapter)
			convey.So(err, convey.ShouldBeNil)
			convey.So(catalog, convey.ShouldNotBeNil)
			convey.So(len(catalog), convey.ShouldEqual, 60)
		})
	})
}

func TestCommon_CrawlContent(t *testing.T) {
	httpmock.Activate()
	t.Cleanup(httpmock.DeactivateAndReset)
	convey.Convey("Common_CrawlContent", t, func() {
		httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244/147927334.html",
			NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_content_1.html")))

		httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244/147927334_2.html",
			NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_content_2.html")))
		httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244/147927334_3.html",
			NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_content_3.html")))
		httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244/147927334_4.html",
			NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_content_4.html")))
		httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244/147927334_5.html",
			NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_content_5.html")))
		httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244/147927334_6.html",
			NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_content_6.html")))
		httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244/147927334_7.html",
			NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_content_7.html")))
		httpmock.RegisterResponder(http.MethodGet, "https://m.axrss.com/book/566244/147927334_8.html",
			NewFileResponder(http.StatusOK, filepath.Join("testdata", "axrss_content_8.html")))
		rules, err := LoadRule("../conf/source.toml")
		convey.So(err, convey.ShouldBeNil)
		ins := NewCommonSpider()

		convey.Convey("Common_CrawlContent_OK", func() {

			rule, ok := rules.Rule("m.axrss.com")
			convey.So(ok, convey.ShouldBeTrue)
			ctx, cancel := context.WithTimeout(context.TODO(), time.Duration(max(rule.Content.WaitTime, 100))*time.Microsecond)
			defer cancel()

			content, err := ins.CrawlContent(ctx, "https://m.axrss.com/book/566244/147927334.html", &rule.Content)

			convey.So(err, convey.ShouldBeNil)
			convey.So(strings.HasPrefix(content, "此岂天为之耶，抑人耶"), convey.ShouldBeTrue)
			convey.So(strings.HasSuffix(content, "朱翊钧附和道：“是啊，东安王真是罪大恶极，死不足惜！”"), convey.ShouldBeTrue)
		})
	})
}

func NewFileResponder(status int, filename string) httpmock.Responder {
	body, err := os.ReadFile(filename)
	if err != nil {
		return httpmock.NewErrorResponder(err)
	}
	return httpmock.NewBytesResponder(status, body)
}
