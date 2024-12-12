package util

import (
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestResolveFullURL(t *testing.T) {
	convey.Convey("ResolveFullURL", t, func() {
		// 示例基准 URL
		baseURL := "https://www.01xs.com/xiaoshuo/106642/"

		convey.Convey("ResolveFullURL_OK", func() {
			// 示例相对路径和完整 URL
			relativePaths := []string{
				"/xiaoshuo/106642/2.html",   // 相对路径
				"https://www.01xs.com/page", // 完整 URL
				"../106641/1.html",          // 相对路径带 ..
				"./3.html",                  // 当前目录下的相对路径
			}

			// 处理每个路径
			for _, relative := range relativePaths {
				fullURL, err := ResolveFullURL(baseURL, relative)
				convey.So(err, convey.ShouldBeNil)
				convey.So(strings.HasPrefix(fullURL, "https://www.01xs.com/"), convey.ShouldBeTrue)
			}
		})

		convey.Convey("ResolveFullURL_Err", func() {
			fullURL, err := ResolveFullURL(baseURL, "https://www.baidu.com/a.html")
			convey.So(err, convey.ShouldBeNil)
			convey.So(strings.HasPrefix(fullURL, "https://www.01xs.com/"), convey.ShouldBeFalse)
		})

	})
}
