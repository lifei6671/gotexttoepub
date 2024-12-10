package goepub

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	goepub "github.com/go-shiori/go-epub"

	"github.com/lifei6671/gotexttoepub/internal/util"
)

type epubConverter struct{}

func (c *epubConverter) Convert(_ context.Context, book *Book) error {
	err := book.FullDefault()
	//解析TXT文件
	e, err := c.parse(book)
	if err != nil {
		return err
	}
	//设置封面
	err = c.setCover(book, e)
	if err != nil {
		return err
	}
	//生产epub文件
	err = c.WriteTo(book, e)
	if err != nil {
		return err
	}
	return err
}

func (c *epubConverter) WriteTo(book *Book, e *goepub.Epub) error {
	if len(book.Volumes) == 0 {
		return errors.New("卷不能为空")
	}
	var err error
	// 按照卷和章生成 EPUB
	for i, vol := range book.Volumes {
		parentFilename := ""
		if vol.Title != "" {
			internalFilename := fmt.Sprintf("volume%d.xhtml", i)
			parentFilename, err = e.AddSection(fmt.Sprintf("<h1>%s</h1>", vol.Title), vol.Title, internalFilename, "")
			if err != nil {
				log.Printf("添加卷失败 -> %v", err)
				return err
			}
		}
		for j, ch := range vol.Chapters {
			// 如果第一个卷的第一个章节不是标题，则设置小说简介
			if i == 0 && j == 0 && vol.Title == "" && book.IntroRegex != nil && !book.IntroRegex.MatchString(ch.Title) {
				e.SetDescription(removeHTMLTags(ch.Content.String()))
			}
			chapterFilename := fmt.Sprintf("volume%d_chapter%d.xhtml", i, j)
			_, err = e.AddSubSection(parentFilename, fmt.Sprintf("<h2>%s</h2>%s", ch.Title, ch.Content.String()), ch.Title, chapterFilename, "")
			if err != nil {
				log.Printf("添加章节失败 ->卷：%s - 章: %s - 错误: %v", vol.Title, ch.Title, err)
				return err
			}
		}
	}

	return nil
}

func (c *epubConverter) parse(book *Book) (*goepub.Epub, error) {
	// 变量定义
	var (
		title      string
		author     string
		currentVol *Volume
		currentCh  *Chapter
	)

	f, err := os.Open(book.Filename)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败:%s - %w", book.Filename, err)
	}
	defer util.SaleClose(f)
	e, err := goepub.NewEpub("")
	if err != nil {
		return nil, fmt.Errorf("创建 EPUB 失败:%w", err)
	}
	// 行处理
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// 判断标题
		if title == "" && regexp.MustCompile(TitlePattern).MatchString(line) {
			title = line
			e.SetTitle(title)
			log.Printf("小说标题: %s", title)
			continue
		}

		// 判断作者
		if author == "" && regexp.MustCompile(AuthorPattern).MatchString(line) {
			matches := regexp.MustCompile(AuthorPattern).FindStringSubmatch(line)
			if len(matches) > 1 {
				author = strings.TrimSpace(matches[1])
				e.SetAuthor(author)
				log.Printf("小说作者: %s", author)
			}
			continue
		}

		// 判断卷标题
		if book.VolumeRegex != nil && book.VolumeRegex.MatchString(line) {
			if currentCh != nil && currentVol != nil {
				currentVol.Chapters = append(currentVol.Chapters, *currentCh)
				currentCh = nil
			}
			// 处理当前卷
			if currentVol != nil {
				book.Volumes = append(book.Volumes, *currentVol)
			}
			log.Printf("解析卷: %s", line)
			currentVol = &Volume{Title: line}
			continue
		}
		// 判断章节
		if book.ChapterRegex != nil && book.ChapterRegex.MatchString(line) {
			if currentVol == nil {
				currentVol = &Volume{}
			}
			// 处理当前章节
			if currentCh != nil && currentVol != nil {
				currentVol.Chapters = append(currentVol.Chapters, *currentCh)
			}
			log.Printf("解析章节: %s", line)
			currentCh = &Chapter{Title: line}
			continue
		}
		// 处理一些特殊章节
		if book.ExtraRegex != nil && book.ExtraRegex.MatchString(line) {
			if currentVol == nil {
				currentVol = &Volume{}
			}
			if currentCh != nil {
				currentVol.Chapters = append(currentVol.Chapters, *currentCh)
			}
			log.Printf("解析特殊章节: %s", line)
			currentCh = &Chapter{Title: line}
			continue
		}

		if regexp.MustCompile(ExtraPattern).MatchString(line) {
			// 将番外视为一个独立章节
			if currentCh != nil && currentVol != nil {
				currentVol.Chapters = append(currentVol.Chapters, *currentCh)
				currentCh = nil
			}
			if currentVol == nil {
				currentVol = &Volume{Title: "番外"}
			}
			currentCh = &Chapter{Title: line}
			continue
		}
		if currentCh != nil {
			// 章节内容
			lineText := strings.ReplaceAll(strings.ReplaceAll(line, "<", "&lt;"), ">", "&gt;")
			currentCh.Content.WriteString("<p style=\"text-indent:2em\">" + lineText + "</p>\n")
		}

	}

	// 处理最后一个卷和章节
	if currentCh != nil && currentVol != nil {
		currentVol.Chapters = append(currentVol.Chapters, *currentCh)
	}
	if currentVol != nil {
		book.Volumes = append(book.Volumes, *currentVol)
	}

	return e, nil
}

func (c *epubConverter) setCover(book *Book, e *goepub.Epub) error {
	cover := book.Cover
	if util.IsURL(book.Cover) {
		log.Printf("使用网络图片作为封面 -> %s", book.Cover)
		req, err := http.NewRequest(http.MethodGet, book.Cover, nil)
		if err != nil {
			log.Printf("创建网络请求失败 -> %v", err)
			return err
		}
		req.Header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0")
		req.Header.Add("Referer", book.Cover)
		req.Header.Add("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7\n")
		req.Header.Add("Accept-Language", "zh-CN,zh;q=0.9")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("网络请求失败 -> %v", err)
			return err
		} else if resp.StatusCode != 200 {
			log.Printf("网络请求失败 -> %s", resp.Status)
			return fmt.Errorf("网络请求失败")
		}
		defer util.SaleClose(resp.Body)

		cover = filepath.Join(os.TempDir(), fmt.Sprintf("%d.jpg", time.Now().UnixNano()))
		f, err := os.Create(cover)
		if err != nil {
			log.Printf("创建临时文件失败 -> %s %v", cover, err)
			return err
		} else {
			_, _ = io.Copy(f, resp.Body)
			util.SaleClose(f)
		}
	}
	cover, err := e.AddImage(cover, filepath.Base(cover))
	if err != nil {
		log.Printf("添加图片失败 -> %s %v", book.Cover, err)
		return err
	} else {
		err = e.SetCover(cover, "")
		if err != nil {
			log.Printf("设置封面失败 -> %v", err)
			return err
		}
	}
	return nil
}

func NewEPUBConverter() Converter {
	return &epubConverter{}
}
