package spider

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestLoadRule(t *testing.T) {
	convey.Convey("LoadRule", t, func() {
		convey.Convey("LoadRule_OK", func() {
			list, err := LoadRule("../conf/source.toml")

			convey.So(err, convey.ShouldBeNil)
			convey.So(list, convey.ShouldNotBeNil)
		})
	})
}
