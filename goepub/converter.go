package goepub

import "context"

// Converter 定义统一的电子书转换接口。
// 当前项目主要实现为 EPUB 转换器，但这个抽象允许后续继续扩展为其他输出格式。
type Converter interface {
	// Convert 按照 Book 中的配置将输入文本转换为目标格式。
	Convert(ctx context.Context, book *Book) error
}
