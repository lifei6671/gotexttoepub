package spider

import "context"

type xBiQuGe struct {
}

func (x *xBiQuGe) CrawlMetadata(ctx context.Context, urlStr string, rule *MetadataRule) (*Metadata, error) {
	//TODO implement me
	panic("implement me")
}

func (x *xBiQuGe) CrawlCatalog(ctx context.Context, urlStr string, rule *ChapterRule) ([]*Catalog, error) {
	//TODO implement me
	panic("implement me")
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
