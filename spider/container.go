package spider

import (
	"fmt"
)

var DefaultContainer = NewContainer()

type Container struct {
	spider map[string]Spider
}

func NewContainer() *Container {
	return &Container{
		spider: map[string]Spider{},
	}
}

func (c *Container) Register(s Spider) error {
	if _, ok := c.spider[s.Name()]; ok {
		return fmt.Errorf("spider already exists:%s", s.Name())
	}
	c.spider[s.Name()] = s
	return nil
}

func (c *Container) Spider(name string) (Spider, bool) {
	if s, ok := c.spider[name]; ok {
		return s, true
	}
	if s, ok := c.spider["common"]; ok {
		return s, false
	}
	return nil, false
}

func init() {
	_ = DefaultContainer.Register(NewCommonSpider())
}
