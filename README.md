# go-texttoepub

`go-texttoepub` 是一个将 TXT 小说转换为 EPUB 电子书的 Go 命令行工具，也可以作为库嵌入到其他项目中使用。

项目的目标不是做复杂排版，而是提供一条足够稳定的转换链路：

- 从 TXT 中提取书名、作者
- 按卷、按章节切分正文
- 自动生成 EPUB 目录结构
- 支持本地封面和网络封面
- 支持内置规则和配置文件叠加的识别方式
- 支持“通用内置规则 + 命名预设 + 配置文件覆盖”三级规则体系
- 兼容旧版链式调用方式

## 适用场景

这个工具更适合“结构比较规整”的小说文本，例如：

- 第一行是书名
- 作者行形如 `作者：xxx`
- 章节标题形如 `第一章 xxx`
- 卷标题形如 `第一卷 xxx`

如果你的 TXT 格式不完全一致，也可以通过命令行参数覆盖默认正则，或者加载规则配置文件来补充少量特殊站点规则。

## 项目结构

当前主要代码分为三层：

- `main.go`
  - 程序入口，负责启动 CLI
- `cmd/`
  - 命令行参数解析与命令分发
- `goepub/`
  - 核心转换逻辑，包括文本解析、卷章组织、EPUB 输出和资源处理

## 功能说明

### 1. 自动提取元信息

默认会尝试从 TXT 中提取：

- 书名
  - 默认取第一个非空白行
- 作者
  - 默认匹配 `作者：xxx`
- 简介
  - 默认尝试从 `简介`、`内容简介`、`楔子`、`引子`、`序`、`序言` 等章节推导

### 2. 自动切分卷与章节

默认规则：

- 卷标题正则：
  - `^(第[一二三四五六七八九十百零0-9]+(卷|部|集))([\s　:：\-—].{0,30})?$`
- 章节标题正则：
  - `^((第[一二三四五六七八九十百千万零0-9]+(章|回|节))|(完本感言)).{0,40}$`
- 番外标题正则：
  - `^番外.{0,30}$`

此外，内置规则会自动忽略一部分明显不是卷标题的作者说明行，例如：

- `第一卷接近尾声，三九要再整理一遍后面的大纲，今天只有两更~`
- `今天只有一更`
- `请假说明`

### 3. 支持封面

封面支持两种来源：

- 本地图片路径
- 网络图片 URL

如果是网络图片，程序会先下载到临时文件，再写入 EPUB。

### 4. 兼容长段落 TXT

有些小说 TXT 会出现很长的一整行正文，默认 `bufio.Scanner` 很容易报错。项目内部已经放大扫描缓冲区，避免常见长行文本转换失败。

## 安装与编译

### 方式一：拉取源码后编译

```bash
git clone https://github.com/lifei6671/gotexttoepub.git
cd gotexttoepub
go build ./...
```

### 方式二：只编译入口文件

```bash
go build main.go
```

编译完成后会生成可执行文件：

- Windows：`gotexttoepub.exe`
- Linux / macOS：`gotexttoepub`

## 命令行使用

### 查看可用渠道

```bash
gotexttoepub rules channels --rule-config="./rules.example.toml"
```

如果不传 `--rule-config`，程序会按自动发现规则文件的逻辑去列出当前可用渠道。

如果你想同时看每个渠道继承了哪些 preset、覆盖了哪些字段，可以加：

```bash
gotexttoepub rules channels --rule-config="./rules.example.toml" --show-details
```

如果你想直接看某个渠道最终合并后的完整有效规则，可以用：

```bash
gotexttoepub rules show --rule-config="./rules.example.toml" --rule-channel="qidian"
```

### 基础示例

```bash
gotexttoepub epub \
  -file="~/fiction.txt" \
  -encoding="auto" \
  -rule-channel="qidian" \
  -rule-preset-mode="suggest" \
  -cover="https://example.com/cover.jpg" \
  -output="~/fiction.epub"
```

### 指定章节正则

```bash
gotexttoepub epub \
  -file="~/fiction.txt" \
  -title-regexp="^书名[:：](.*)$" \
  -author-regexp="^作者名[:：](.*)$" \
  -chapter-regexp="^第.*?章.*$" \
  -output="~/fiction.epub"
```

### 同时指定卷和章节正则

```bash
gotexttoepub epub \
  -file="~/fiction.txt" \
  -volume-regexp="^第.*卷.*$" \
  -chapter-regexp="^第.*章.*$" \
  -rule-config="./rules.example.toml" \
  -rule-channel="fanqie" \
  -output="./output"
```

### 自动探测并直接应用预设

```bash
gotexttoepub epub \
  -file="./novel.txt" \
  -rule-preset-mode="apply" \
  -output="./novel.epub"
```

如果 `-output` 指向目录，程序会自动按书名生成最终的 `.epub` 文件。

## 参数说明

### 当前参数

- `-file`, `-f`
  - 输入 TXT 文件路径，必填
- `-cover`, `-img`
  - 封面图片路径或 URL
- `-author`
  - 手动指定作者，留空则自动解析
- `-title-regexp`
  - 书名解析正则，支持使用捕获组提取最终书名
- `-author-regexp`
  - 作者解析正则，支持使用捕获组提取最终作者名
- `-lang`
  - EPUB 语言，默认 `zh-CN`
- `-encoding`, `-charset`
  - 输入 TXT 编码，默认 `auto`，支持 `auto`、`utf-8`、`gbk`、`gb18030`
- `-chapter-regexp`, `-r`
  - 自定义章节匹配正则
- `-volume-regexp`, `-vr`
  - 自定义卷匹配正则
- `-rule-config`, `-config`
  - 规则配置文件路径，使用带注释的 TOML 格式，在内置规则基础上做覆盖
- `-rule-preset`, `-preset`
  - 内置规则预设名称，多个预设使用逗号分隔
- `-rule-channel`, `-channel`
  - 规则配置中的渠道名称，例如 `default`、`qidian`、`fanqie`
- `-rule-preset-mode`
  - 自动探测预设的行为模式，支持 `off`、`suggest`、`apply`，默认 `suggest`
- `-output`, `-o`
  - 输出路径，可传文件路径或目录

### 兼容旧参数

为了兼容旧版本，下面这些参数别名仍然有效：

- `-regexr`
  - 等价于 `-chapter-regexp`
- `-title-regexp`
  - 等价于 `-chapter-regexp`
- `-chapter-pattern`
  - 等价于 `-chapter-regexp`
- `-volume-pattern`
  - 等价于 `-volume-regexp`

## 作为库使用

### 新版推荐用法

推荐直接使用统一的 `Book + Converter` 抽象：

```go
package main

import (
	"context"
	"regexp"

	"github.com/lifei6671/gotexttoepub/goepub"
)

func main() {
	book := &goepub.Book{
		Filename: "fiction.txt",
		Output:   "fiction.epub",
		Cover:    "cover.jpg",
		Author:   "某作者",
		ChapterRegex: regexp.MustCompile(`^第.+章`),
	}

	_ = goepub.NewEPUBConverter().Convert(context.Background(), book)
}
```

### 旧版链式调用

项目仍然保留旧版链式 API：

```go
converter := goepub.NewConverter()
err := converter.
	SetContent("fiction.txt").
	SetCover("cover.jpg").
	SetRegExp(regexp.MustCompile(`^第.+章`)).
	Convert("fiction.epub")
```

这层兼容接口内部最终也会走新的统一转换流程。

## 默认解析规则

项目内部默认会使用以下规则：

- 书名：
  - 首个非空白行
- 作者：
  - `^作者[:：](.*)$`
- 简介章节：
  - `^(内容简介|简介|楔子|引子|序|序言)$`
- 卷标题：
  - `^(第[一二三四五六七八九十百零0-9]+(卷|部|集))([\s　:：\-—].{0,30})?$`
- 章节标题：
  - `^((第[一二三四五六七八九十百千万零0-9]+(章|回|节))|(完本感言)).{0,40}$`
- 番外：
  - `^番外.{0,30}$`

此外，以下标题也会被当作独立章节处理：

- `楔子`
- `卷首语`
- `序`
- `引子`
- `序言`
- `完本感言`
- `楔子语`
- 以及以 `简介`、`内容简介` 开头的行

## 规则系统

项目现在采用“通用内置规则 + 命名预设 + 可选配置文件覆盖”的方式工作：

- 大多数常见中文小说 TXT，直接使用内置规则即可
- 如果某个来源有比较固定的噪音行或说明格式，可以先叠加命名预设
- 少量格式特殊的来源，可以再通过 TOML 配置文件补充忽略词、忽略正则和简介前缀
- 如果你已经明确知道某个站点的卷名、章节名格式，也可以继续用命令行参数直接覆盖正则

配置文件示例见 [rules.example.toml](E:\wx_lifeilin\github.com\lifei6671\gotexttoepub\rules.example.toml)。

规则文件现在支持“多渠道”写法。你可以在同一份 TOML 里维护多个渠道块，例如：

- `default`
- `qidian`
- `fanqie`
- `jjwxc`

然后通过 `-rule-channel` 指定要使用哪个渠道。如果不指定：

- 优先使用规则文件中的 `default_channel`
- 如果没写 `default_channel`，但存在 `[channels.default]`，则使用 `default`
- 如果两者都没有，就只使用顶层全局规则

如果你没有显式传 `-rule-config`，程序还会自动尝试加载以下规则文件：

- 用户配置目录：
  - Windows: `%AppData%\gotexttoepub\rules.toml`
  - macOS: `~/Library/Application Support/gotexttoepub/rules.toml`
  - Linux: `~/.config/gotexttoepub/rules.toml`
- 程序同目录：
  - `rules.toml`
  - `gotexttoepub.rules.toml`
  - `<可执行文件名>.rules.toml`

加载优先级从低到高大致是：

- 内置默认规则
- 手动指定的命名预设
- 自动探测到的命名预设
- 用户配置目录规则
- 程序同目录规则
- `-rule-config` 显式指定的规则文件
- `-rule-channel` 选中的渠道块
- `-title-regexp` / `-author-regexp` / `-volume-regexp` / `-chapter-regexp` 等命令行正则

如果你没有手动指定 `-rule-preset`，程序还会根据文本前几百行自动做一次特征探测：

- `off`
  - 关闭自动探测
- `suggest`
  - 只输出推荐预设日志，不直接改解析规则
- `apply`
  - 自动将探测到的预设叠加到当前规则中

当前内置的命名预设包括：

- `serial`
  - 通用连载预设，补充作者说明、请假条、加更规则、更新时间说明等噪音行
- `qidian`
  - 起点类预设，补充上架感言、求月票、求订阅、单章求票等规则
- `fanqie`
  - 番茄类预设，补充作者说明、催更说明、更新通知等规则
- `jjwxc`
  - 晋江类预设，补充入V公告、谢绝扒榜、阅读提示、`文案：` 前缀等规则

你可以直接在命令行里组合使用多个预设：

```bash
gotexttoepub epub \
  -file="./novel.txt" \
  -rule-preset="qidian,serial" \
  -output="./novel.epub"
```

当前支持的配置字段包括：

- `default_channel`
  - 未显式传 `-rule-channel` 时默认使用的渠道
- `[channels.<name>]`
  - 某个渠道自己的规则块，例如 `[channels.qidian]`
- `extends_presets`
  - 让当前层规则先继承一个或多个内置预设，再叠加自己的覆盖内容
- `title_regex`
  - 自定义书名识别正则，建议使用第一个捕获组返回真正书名
- `title_author_regex`
  - 自定义“书名 + 作者”同一行的识别正则
- `author_regex`
  - 自定义作者行识别正则
- `volume_regex`
  - 自定义卷标题识别正则
- `chapter_regex`
  - 自定义章节标题识别正则
- `extra_regex`
  - 自定义番外标题识别正则
- `intro_regex`
  - 自定义简介章节标题识别正则
- `intro_prefixes`
  - 自定义简介前缀，例如 `小说简介：`
- `special_chapter_titles`
  - 额外补充需要单独作为章节处理的标题
- `ignored_line_patterns`
  - 按正则忽略整行内容，适合处理作者说明、请假条、更新提示
- `ignored_line_contains`
  - 按关键字忽略整行内容，适合处理格式不太固定的杂讯行

也就是说，文章标题、作者、卷、章节这些核心识别规则，既可以用命令行覆盖，也可以直接写进 TOML 配置文件里。

例如：

```toml
default_channel = "qidian"

[channels.qidian]
title_regex = "^书名[:：](.*)$"
author_regex = "^作者名[:：](.*)$"
volume_regex = "^(正文卷|终卷).*$"
chapter_regex = "^(第[0-9]+章|Chapter\\s+[0-9]+).*$"
```

推荐使用 TOML 的原因也很直接：

- 可以写注释，方便记录“这个规则是为了屏蔽哪一类作者说明”
- 对手工维护更友好，尤其适合长期积累不同来源的规则
- 数组和字符串配置足够直观，不需要额外转义成复杂 JSON 结构

示例：

```bash
gotexttoepub epub \
  -file="./我不是戏神.txt" \
  -encoding="auto" \
  -rule-channel="fanqie" \
  -rule-preset-mode="apply" \
  -rule-config="./rules.example.toml" \
  -output="./我不是戏神.epub"
```

## 注意事项

### 1. TXT 编码问题

项目现在支持自动识别并转换常见中文 TXT 编码：

- UTF-8
- GBK
- GB18030

默认使用 `-encoding=auto`，大多数中文小说文本不需要再手动转码。

### 2. 文本格式越规整，转换效果越稳定

这个工具不是基于 AI 做语义识别，而是基于规则和正则分段，所以原始 TXT 的格式越统一，转换结果越稳定。

### 3. 输出文件名会自动清理非法字符

如果书名里包含 Windows 不允许的文件名字符，例如 `:`、`*`、`?` 等，程序会自动替换，避免写文件失败。

### 4. 网络封面依赖外部可访问性

如果传入的是远程封面 URL，目标地址需要可访问，否则转换会因封面下载失败而报错。

## 开发与验证

常用命令：

```bash
go test ./...
go build ./...
```

## License

本项目使用 [MIT License](E:\wx_lifeilin\github.com\lifei6671\gotexttoepub\LICENSE)。
