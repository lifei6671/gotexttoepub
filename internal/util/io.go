package util

import (
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
)

// SaleClose 安全的关闭
func SaleClose(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

// IsURL 判断是否是网络地址
func IsURL(input string) bool {
	parsedURL, err := url.Parse(input)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false
	}
	// 常见协议判断
	supportedSchemes := []string{"http", "https", "ftp"}
	for _, scheme := range supportedSchemes {
		if strings.EqualFold(parsedURL.Scheme, scheme) {
			return true
		}
	}
	return false
}

// ResolvePath 解析包含的路径，将相对路径转换为绝对路径
func ResolvePath(baseDir string, path string) (string, error) {
	if filepath.IsAbs(path) {
		// 绝对路径，直接使用
		return path, nil
	}
	// 相对路径，基于 baseDir 转换为绝对路径
	absPath, err := filepath.Abs(filepath.Join(baseDir, path))
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %s: %w", path, err)
	}
	return absPath, nil
}
