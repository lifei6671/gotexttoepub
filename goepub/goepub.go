package goepub

import (
	goepub "github.com/bmaupin/go-epub"
	"path/filepath"
)

type epub struct {
	p string
}

func NewConverter(bookName string) *epub {
	return &epub{}
}

func (e *epub) Convert(bookName, author, coverUrl, regexr string) error {

	epub := goepub.NewEpub(bookName)
	epub.SetLang("zh_cn")
	epub.SetAuthor(author)
	cover, err := epub.AddImage(coverUrl, filepath.Base(coverUrl))
	if err != nil {
		log.Printf("【%s】添加图片失败 -> %s %s", bookName, coverUrl, err)
		return err
	} else {
		epub.SetCover(cover, "")
	}

	return nil
}
