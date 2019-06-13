# go-texttoepub

将TXT格式的小说转换为epub格式。

## 功能列表

- 自动按章节切分
- 自动提取章节标题
- 指定封面链接自动下载封面

## 使用

1、 拉取源码

```bash
go get github.com/lifei6671/gotexttoepub
```

2、编译

```bash
go build main.go
```

3、转换

```bash
gotexttoepub epub -file="~/fiction.txt" -cover="https://www.baidu.com/logo.img" -regexr="(^第.*?章.*)" -output="~/fiction.epub"
```