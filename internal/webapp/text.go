package webapp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var (
	utf8BOM    = []byte{0xEF, 0xBB, 0xBF}
	utf16LEBOM = []byte{0xFF, 0xFE}
	utf16BEBOM = []byte{0xFE, 0xFF}
)

// normalizeUploadedTXT 将已落盘的 TXT 统一为 UTF-8。
// 仅接受可以明确判定的 UTF-8、带 BOM 的 UTF-16 和语法合法的 GB18030；
// 其他编码不能可靠识别，避免把二进制内容误当成小说正文。
func normalizeUploadedTXT(file *os.File) (int64, error) {
	raw, err := os.ReadFile(file.Name())
	if err != nil {
		return 0, fmt.Errorf("读取 TXT 文件失败: %w", err)
	}

	utf8Text, converted, err := decodeUploadedTXT(raw)
	if err != nil {
		return 0, err
	}
	if !converted {
		return int64(len(raw)), nil
	}
	if len(utf8Text) == 0 {
		return 0, fmt.Errorf("%w: TXT 文件不能为空", errInvalidUpload)
	}
	if err := rewriteUploadedTXT(file, utf8Text); err != nil {
		return 0, err
	}
	return int64(len(utf8Text)), nil
}

func decodeUploadedTXT(raw []byte) ([]byte, bool, error) {
	switch {
	case bytes.HasPrefix(raw, utf8BOM):
		text := raw[len(utf8BOM):]
		if !utf8.Valid(text) || bytes.IndexByte(text, 0) >= 0 {
			return nil, false, fmt.Errorf("%w: UTF-8 文本转换失败", errInvalidUpload)
		}
		return text, true, nil
	case bytes.HasPrefix(raw, utf16LEBOM):
		text, err := decodeUTF16Text(raw[len(utf16LEBOM):], binary.LittleEndian)
		if err != nil {
			return nil, false, fmt.Errorf("%w: UTF-16LE 文本转换失败", errInvalidUpload)
		}
		return []byte(text), true, nil
	case bytes.HasPrefix(raw, utf16BEBOM):
		text, err := decodeUTF16Text(raw[len(utf16BEBOM):], binary.BigEndian)
		if err != nil {
			return nil, false, fmt.Errorf("%w: UTF-16BE 文本转换失败", errInvalidUpload)
		}
		return []byte(text), true, nil
	case utf8.Valid(raw):
		if bytes.IndexByte(raw, 0) >= 0 {
			return nil, false, fmt.Errorf("%w: TXT 文件包含 NUL 字符", errInvalidUpload)
		}
		return raw, false, nil
	case validGB18030(raw):
		text, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw)
		if err != nil || bytes.IndexByte(text, 0) >= 0 || bytes.ContainsRune(text, utf8.RuneError) {
			return nil, false, fmt.Errorf("%w: GB18030 文本转换失败", errInvalidUpload)
		}
		return text, true, nil
	default:
		return nil, false, fmt.Errorf("%w: 无法识别 TXT 编码，请转换为 UTF-8 后重试", errInvalidUpload)
	}
}

func decodeUTF16Text(raw []byte, order binary.ByteOrder) (string, error) {
	if len(raw)%2 != 0 {
		return "", io.ErrUnexpectedEOF
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = order.Uint16(raw[i*2:])
	}
	for i := 0; i < len(units); i++ {
		switch {
		case 0xD800 <= units[i] && units[i] <= 0xDBFF:
			if i+1 >= len(units) || units[i+1] < 0xDC00 || units[i+1] > 0xDFFF {
				return "", fmt.Errorf("unpaired high surrogate")
			}
			i++
		case 0xDC00 <= units[i] && units[i] <= 0xDFFF:
			return "", fmt.Errorf("unpaired low surrogate")
		}
	}
	text := string(utf16.Decode(units))
	if strings.IndexByte(text, 0) >= 0 {
		return "", fmt.Errorf("NUL character")
	}
	return text, nil
}

func validGB18030(raw []byte) bool {
	for i := 0; i < len(raw); {
		switch {
		case raw[i] <= 0x7F:
			if raw[i] == 0 {
				return false
			}
			i++
		case raw[i] == 0x80:
			// GB18030 解码器将 CP936 扩展字节 0x80 解释为欧元符号。
			i++
		case raw[i] >= 0x81 && raw[i] <= 0xFE:
			if i+1 >= len(raw) {
				return false
			}
			if raw[i+1] >= 0x30 && raw[i+1] <= 0x39 {
				if i+3 >= len(raw) || raw[i+2] < 0x81 || raw[i+2] > 0xFE || raw[i+3] < 0x30 || raw[i+3] > 0x39 {
					return false
				}
				i += 4
				continue
			}
			if raw[i+1] == 0x7F || raw[i+1] < 0x40 || raw[i+1] > 0xFE {
				return false
			}
			i += 2
		default:
			return false
		}
	}
	return true
}

func rewriteUploadedTXT(file *os.File, text []byte) error {
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("准备 UTF-8 文本失败: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("准备 UTF-8 文本失败: %w", err)
	}
	if written, err := file.Write(text); err != nil {
		return fmt.Errorf("写入 UTF-8 文本失败: %w", err)
	} else if written != len(text) {
		return fmt.Errorf("写入 UTF-8 文本失败: %w", io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步 UTF-8 文本失败: %w", err)
	}
	return nil
}
