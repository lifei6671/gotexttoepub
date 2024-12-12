package spider

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/smartystreets/goconvey/convey"
)

func TestXBiQuGe_CrawlMetadata(t *testing.T) {
	httpmock.Activate()
	t.Cleanup(httpmock.DeactivateAndReset)
	body, _ := os.ReadFile(filepath.Join("testdata", "biquge_catalog.html"))
	httpmock.RegisterResponder(http.MethodGet, "https://www.01xs.com/xiaoshuo/106642/",
		httpmock.NewBytesResponder(http.StatusOK, body))
	httpmock.RegisterResponder(http.MethodGet, "https://www.01xs.com/xiaoshuo/106643/",
		httpmock.NewBytesResponder(http.StatusForbidden, nil))

	convey.Convey("XBiQuGe_CrawlMetadata", t, func() {
		ctx := context.TODO()
		rules, err := LoadRule("../conf/source.toml")
		convey.So(err, convey.ShouldBeNil)
		var rule *BookRule
		for _, r := range rules {
			if r.RuleName == "www.01xs.com" {
				rule = r
				break
			}
		}
		convey.So(rule, convey.ShouldNotBeNil)

		ins := NewXBiQuGe()
		convey.Convey("XBiQuGe_CrawlMetadata_OK", func() {
			metadata, err := ins.CrawlMetadata(ctx, "https://www.01xs.com/xiaoshuo/106642/", &rule.Metadata)
			convey.So(err, convey.ShouldBeNil)
			convey.So(metadata, convey.ShouldNotBeNil)
			convey.So(metadata.Name, convey.ShouldEqual, "梦回大明春")
			convey.So(metadata.Author, convey.ShouldEqual, "王梓钧")
			convey.So(metadata.Category, convey.ShouldEqual, "军史穿越")
			convey.So(strings.HasPrefix(metadata.Intro, "穿越到大明朝"), convey.ShouldBeTrue)
		})
		convey.Convey("XBiQuGe_CrawlMetadata_Err", func() {
			metadata, err := ins.CrawlMetadata(ctx, "https://www.01xs.com/xiaoshuo/106643/", &rule.Metadata)
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(metadata, convey.ShouldBeNil)
		})
	})
}

func TestXBiQuGe_CrawlCatalog(t *testing.T) {
	httpmock.Activate()
	t.Cleanup(httpmock.DeactivateAndReset)
	body, err := os.ReadFile("./testdata/biquge_catalog.html")
	log.Println(err)
	httpmock.RegisterResponder(http.MethodGet, "https://www.01xs.com/xiaoshuo/106642/",
		httpmock.NewBytesResponder(http.StatusOK, body))

	convey.Convey("XBiQuGe_CrawlCatalog", t, func() {
		ctx := context.TODO()
		rules, err := LoadRule("../conf/source.toml")
		convey.So(err, convey.ShouldBeNil)
		var rule *BookRule
		for _, r := range rules {
			if r.RuleName == "www.01xs.com" {
				rule = r
				break
			}
		}
		convey.So(rule, convey.ShouldNotBeNil)
		ins := NewXBiQuGe()

		convey.Convey("XBiQuGe_CrawlCatalog_OK", func() {
			catalog, err := ins.CrawlCatalog(ctx, "https://www.01xs.com/xiaoshuo/106642/", &rule.Chapter)
			convey.So(err, convey.ShouldBeNil)
			convey.So(catalog, convey.ShouldNotBeNil)
			convey.So(len(catalog), convey.ShouldEqual, 799)
		})

		convey.Convey("XBiQuGe_CrawlCatalog_Page_OK", func() {

		})
	})
}
