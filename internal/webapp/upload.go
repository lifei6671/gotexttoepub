package webapp

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	errInvalidUpload   = errors.New("上传内容无效")
	errUploadTooLarge  = errors.New("TXT 文件超过大小限制")
	errCoverTooLarge   = errors.New("封面文件超过大小限制")
	errInvalidCover    = errors.New("封面文件无效")
	maxCoverImagePixel = uint64(25_000_000)
)

type uploadedRequest struct {
	InputPath    string
	OriginalName string
	InputSize    int64
	CoverPath    string
	CoverURL     string
}

func parseUpload(w http.ResponseWriter, r *http.Request, incomingDir string, maxUploadBytes, maxCoverBytes int64) (_ *uploadedRequest, retErr error) {
	if maxUploadBytes <= 0 || maxCoverBytes <= 0 {
		return nil, fmt.Errorf("%w: 服务端上传限制无效", errInvalidUpload)
	}
	if err := os.MkdirAll(incomingDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}
	if err := os.Chmod(incomingDir, 0o700); err != nil {
		return nil, fmt.Errorf("设置上传目录权限失败: %w", err)
	}

	// 额外空间用于 multipart 边界、字段名和封面 URL。
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+maxCoverBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("%w: 请求必须使用 multipart/form-data", errInvalidUpload)
	}

	var result uploadedRequest
	defer func() {
		if retErr != nil {
			_ = os.Remove(result.InputPath)
			_ = os.Remove(result.CoverPath)
		}
	}()

	seenFile, seenCoverFile, seenCoverURL := false, false, false
	partCount := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				return nil, errUploadTooLarge
			}
			return nil, fmt.Errorf("%w: 读取上传内容失败", errInvalidUpload)
		}
		partCount++
		if partCount > 3 {
			_ = part.Close()
			return nil, fmt.Errorf("%w: 只允许 file、cover_file 和 cover_url 三个字段", errInvalidUpload)
		}

		switch part.FormName() {
		case "file":
			if seenFile || part.FileName() == "" {
				_ = part.Close()
				return nil, fmt.Errorf("%w: file 字段必须且只能上传一个文件", errInvalidUpload)
			}
			seenFile = true
			if !strings.EqualFold(filepath.Ext(part.FileName()), ".txt") {
				_ = part.Close()
				return nil, fmt.Errorf("%w: 只支持 .txt 文件", errInvalidUpload)
			}
			path, size, err := writeUploadedTXT(part, incomingDir, maxUploadBytes)
			_ = part.Close()
			if err != nil {
				return nil, err
			}
			result.InputPath = path
			result.InputSize = size
			result.OriginalName = cleanDisplayFilename(part.FileName())
		case "cover_file":
			if seenCoverFile || part.FileName() == "" {
				_ = part.Close()
				return nil, fmt.Errorf("%w: cover_file 字段只能上传一个图片", errInvalidCover)
			}
			seenCoverFile = true
			path, err := writeUploadedCover(part, incomingDir, maxCoverBytes)
			_ = part.Close()
			if err != nil {
				return nil, err
			}
			result.CoverPath = path
		case "cover_url":
			if seenCoverURL || part.FileName() != "" {
				_ = part.Close()
				return nil, fmt.Errorf("%w: cover_url 字段无效", errInvalidUpload)
			}
			seenCoverURL = true
			value, err := io.ReadAll(io.LimitReader(part, 2049))
			_ = part.Close()
			if err != nil {
				return nil, fmt.Errorf("%w: 读取封面链接失败", errInvalidUpload)
			}
			if len(value) > 2048 {
				return nil, fmt.Errorf("%w: 封面链接过长", errInvalidUpload)
			}
			result.CoverURL = strings.TrimSpace(string(value))
		default:
			_ = part.Close()
			return nil, fmt.Errorf("%w: 不支持字段 %q", errInvalidUpload, part.FormName())
		}
	}

	if !seenFile || result.InputPath == "" {
		return nil, fmt.Errorf("%w: 请选择 TXT 文件", errInvalidUpload)
	}
	if result.CoverPath != "" && result.CoverURL != "" {
		return nil, fmt.Errorf("%w: 封面上传和封面链接不能同时使用", errInvalidUpload)
	}
	return &result, nil
}

func writeUploadedTXT(part *multipart.Part, incomingDir string, maxUploadBytes int64) (path string, size int64, retErr error) {
	file, err := os.CreateTemp(incomingDir, "upload-*.txt")
	if err != nil {
		return "", 0, fmt.Errorf("创建上传临时文件失败: %w", err)
	}
	filePath := file.Name()
	path = filePath
	defer func() {
		if closeErr := file.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("关闭上传文件失败: %w", closeErr)
		}
		if retErr != nil {
			_ = os.Remove(filePath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", 0, fmt.Errorf("设置上传文件权限失败: %w", err)
	}

	written, err := io.Copy(file, io.LimitReader(part, maxUploadBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("保存上传文件失败: %w", err)
	}
	if written == 0 {
		return "", 0, fmt.Errorf("%w: TXT 文件不能为空", errInvalidUpload)
	}
	if written > maxUploadBytes {
		return "", 0, errUploadTooLarge
	}
	if err := file.Sync(); err != nil {
		return "", 0, fmt.Errorf("同步上传文件失败: %w", err)
	}
	if err := validateTextSample(file); err != nil {
		return "", 0, err
	}
	return path, written, nil
}

func validateTextSample(file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("检查 TXT 文件失败: %w", err)
	}
	sample := make([]byte, 8192)
	n, err := file.Read(sample)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("检查 TXT 文件失败: %w", err)
	}
	if bytes.IndexByte(sample[:n], 0) >= 0 {
		return fmt.Errorf("%w: 文件包含二进制 NUL 字节", errInvalidUpload)
	}
	return nil
}

func writeUploadedCover(part *multipart.Part, incomingDir string, maxCoverBytes int64) (path string, retErr error) {
	file, err := os.CreateTemp(incomingDir, "cover-*")
	if err != nil {
		return "", fmt.Errorf("创建封面临时文件失败: %w", err)
	}
	tempPath := file.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); retErr == nil && closeErr != nil {
				retErr = fmt.Errorf("关闭封面文件失败: %w", closeErr)
			}
		}
		if retErr != nil {
			_ = os.Remove(tempPath)
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", fmt.Errorf("设置封面文件权限失败: %w", err)
	}

	written, err := io.Copy(file, io.LimitReader(part, maxCoverBytes+1))
	if err != nil {
		return "", fmt.Errorf("保存封面文件失败: %w", err)
	}
	if written == 0 {
		return "", fmt.Errorf("%w: 封面文件不能为空", errInvalidCover)
	}
	if written > maxCoverBytes {
		return "", errCoverTooLarge
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("同步封面文件失败: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("检查封面文件失败: %w", err)
	}

	header := make([]byte, 512)
	n, readErr := io.ReadFull(file, header)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("%w: 封面文件不是图片", errInvalidCover)
	}
	contentType := http.DetectContentType(header[:n])
	var extension, expectedFormat string
	switch contentType {
	case "image/jpeg":
		extension, expectedFormat = ".jpg", "jpeg"
	case "image/png":
		extension, expectedFormat = ".png", "png"
	default:
		return "", fmt.Errorf("%w: 只支持 JPEG 或 PNG 图片", errInvalidCover)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("检查封面文件失败: %w", err)
	}
	config, format, err := image.DecodeConfig(file)
	if err != nil || format != expectedFormat || config.Width <= 0 || config.Height <= 0 {
		return "", fmt.Errorf("%w: 封面文件不是有效 JPEG 或 PNG 图片", errInvalidCover)
	}
	if uint64(config.Width)*uint64(config.Height) > maxCoverImagePixel {
		return "", fmt.Errorf("%w: 封面图片像素超过 2500 万", errInvalidCover)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("关闭封面文件失败: %w", err)
	}
	closed = true
	path = tempPath + extension
	if err := os.Rename(tempPath, path); err != nil {
		return "", fmt.Errorf("保存封面文件失败: %w", err)
	}
	return path, nil
}

func cleanDisplayFilename(name string) string {
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return "novel.txt"
	}
	const maxRunes = 180
	if utf8.RuneCountInString(name) > maxRunes {
		runes := []rune(name)
		name = string(runes[:maxRunes])
	}
	return name
}
