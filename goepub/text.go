package goepub

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	encodingAuto    = "auto"
	encodingUTF8    = "utf-8"
	encodingUTF8Alt = "utf8"
	encodingGBK     = "gbk"
	encodingGB18030 = "gb18030"
)

// normalizeEncodingName 归一化编码名称，方便统一比较。
func normalizeEncodingName(encoding string) string {
	normalized := strings.ToLower(strings.TrimSpace(encoding))
	switch normalized {
	case "", encodingAuto:
		return encodingAuto
	case encodingUTF8Alt:
		return encodingUTF8
	default:
		return normalized
	}
}

// decodeTextContent 将原始字节解码为 UTF-8 字符串。
// 当编码为 auto 时，会优先判断是否为 UTF-8，否则回退到 GB18030。
func decodeTextContent(raw []byte, encoding string) (string, string, error) {
	switch normalizeEncodingName(encoding) {
	case encodingAuto:
		if hasUTF8BOM(raw) {
			return string(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})), encodingUTF8, nil
		}
		if utf8.Valid(raw) {
			return string(raw), encodingUTF8, nil
		}
		decoded, err := decodeGB18030(raw)
		return decoded, encodingGB18030, err
	case encodingUTF8:
		content := raw
		if hasUTF8BOM(content) {
			content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
		}
		if !utf8.Valid(content) {
			return "", "", fmt.Errorf("文件内容不是有效的 UTF-8")
		}
		return string(content), encodingUTF8, nil
	case encodingGBK:
		decoded, err := decodeBytes(raw, simplifiedchinese.GBK.NewDecoder())
		return decoded, encodingGBK, err
	case encodingGB18030:
		decoded, err := decodeGB18030(raw)
		return decoded, encodingGB18030, err
	default:
		return "", "", fmt.Errorf("不支持的文本编码: %s", encoding)
	}
}

// hasUTF8BOM 判断文本是否带有 UTF-8 BOM。
func hasUTF8BOM(raw []byte) bool {
	return len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF
}

// decodeGB18030 使用 GB18030 解码，这个编码向下兼容常见的 GBK 文本。
func decodeGB18030(raw []byte) (string, error) {
	return decodeBytes(raw, simplifiedchinese.GB18030.NewDecoder())
}

// decodeBytes 使用指定解码器把文本转换成 UTF-8 字符串。
func decodeBytes(raw []byte, transformer transform.Transformer) (string, error) {
	decoded, _, err := transform.String(transformer, string(raw))
	if err != nil {
		return "", fmt.Errorf("解码文本失败: %w", err)
	}
	return decoded, nil
}
