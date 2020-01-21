package goepub

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	goepub "github.com/bmaupin/go-epub"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type epub struct {
	txtp string
	*goepub.Epub
	content []*chapter
	author  string
	cover   string
	reg     *regexp.Regexp
}

type chapter struct {
	Title   string
	Content string
}

func NewConverter() *epub {
	return &epub{}
}

func (e *epub) SetContent(path string) *epub {
	e.txtp = path
	return e
}

func (e *epub) SetAuthor(author string) *epub {
	e.author = author
	return e
}

func (e *epub) SetCover(cover string) *epub {
	if strings.HasPrefix(cover, "http://") || strings.HasPrefix(cover, "https://") {
		resp, err := http.Get(cover)
		if err != nil {
			log.Printf("下载小说封面失败 -> %v", err)
			return e
		}
		defer resp.Body.Close()
		ext := filepath.Ext(cover)
		if ext == "" {
			contentType := resp.Header.Get("Content-Type")
			if strings.HasPrefix(contentType, "image/") {
				ext = strings.TrimPrefix(contentType, "image/")
			}
		}
		if ext == "" {
			ext = ".jpg"
		}
		f, err := ioutil.TempFile("", "cover*."+ext)
		if err != nil {
			log.Printf("生成临时文件失败 -> %v", err)
			return e
		}
		defer f.Close()
		_, err = io.Copy(f, resp.Body)
		if err != nil {
			log.Printf("保存封面失败 -> %v", err)
			return e
		}
		e.cover = f.Name()
	} else {
		e.cover = cover
	}
	return e
}

func (e *epub) SetRegExp(regexp *regexp.Regexp) *epub {
	e.reg = regexp
	return e
}

func (e *epub) resolve() error {
	if e.txtp == "" {
		return errors.New("TXT 文件路径不能为空")
	}
	p, err := filepath.Abs(e.txtp)
	if err != nil {
		return err
	}
	e.txtp = p

	if e.reg == nil {
		return errors.New("章节提取正则错误")
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	//scanner := bufio.NewScanner(f)

	index := 0
	line := 0
	body := bytes.NewBufferString("")
	title := ""

	reader := bufio.NewReader(f)

	for {
		lineText, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if err := e.resolveChapter(title, body.String(), index); err != nil {
					log.Printf("解析章节失败 -> %s - %v", title, err)
					return err
				}
				break
			}
			return err
		}

		if line == 0 {
			title = strings.TrimSpace(lineText)
			e.Epub = goepub.NewEpub(title)
			line++
			continue
		}

		if lineText == "" {
			continue
		}
		//解析作者
		if line >= 1 && e.author == "" {
			e.author = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(lineText, "作者"), ":"), "："))
			line++
			continue
		}

		//如果匹配到标题
		if e.reg.MatchString(lineText) || lineText == "楔子" {
			if err := e.resolveChapter(title, body.String(), index); err != nil {
				log.Printf("解析章节失败 -> %s - %v", lineText, err)
				return err
			}
			body.Reset()
			index++
			title = lineText
		} else {
			lineText = strings.ReplaceAll(strings.ReplaceAll(lineText, "<", "&lt;"), ">", "&gt;")
			body.WriteString(lineText)
			body.WriteString("\n")
		}
		line++
	}

	//if err := scanner.Err(); err != nil {
	//	log.Printf("解析章节出错 -> %v", err)
	//	return err
	//}

	return nil
}

func (e *epub) Convert(save string) error {
	if err := e.resolve(); err != nil {
		return err
	}
	e.SetLang("zh-CN")
	e.Epub.SetAuthor(e.author)
	if e.cover != "" {
		cover, err := e.AddImage(e.cover, filepath.Base(e.cover))
		if err != nil {
			log.Printf("【%s】添加图片失败 -> %s %s", e.Title(), e.cover, err)
			return err
		} else {
			e.Epub.SetCover(cover, "")
		}
	}
	if save == "" {
		s, err := filepath.Abs("./" + e.Title() + ".epub")
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Dir(s)); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(s), 0755); err != nil {
				log.Printf("创建目录失败 -> %s", err)
			}
		}
		save = s
	}

	return e.Write(save)
}

func (e *epub) resolveChapter(title, body string, index int) error {

	log.Printf("正在处理第 %d 章 -> %s", index, title)

	s := "<h2>" + title + "</h2>"

	for _, cc := range strings.Split(body, "\n") {
		cc = strings.TrimSpace(cc)
		if cc == "" {
			continue
		}
		s += fmt.Sprintf(`<p style="text-indent:2em">%s</p>`, cc)
	}
	_, err := e.AddSection(s, title, fmt.Sprintf("%d.xhtml", index), "")
	if err != nil {
		log.Printf("添加章节失败 -> %v - %s", err, title)
		return err
	}
	return nil
}
