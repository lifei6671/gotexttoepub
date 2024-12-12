package util

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var isTestFunc = sync.OnceValue(func() bool {
	for _, arg := range os.Args {
		if len(arg) > 6 && arg[:6] == "-test." {
			return true
		}
	}
	return false
})

// IsInTest 判断是否在单元测试环境中运行
func IsInTest() bool {
	return isTestFunc()
}

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

// ResolveFullURL 解析相对路径并补全为完整的 URL
func ResolveFullURL(baseURL, relativePath string) (string, error) {
	if relativePath == "" || strings.HasPrefix(strings.ToLower(relativePath), "javascript:") {
		return "", fmt.Errorf("无效的 href: %s", relativePath)
	}
	// 解析相对路径
	relative, err := url.Parse(relativePath)
	if err != nil {
		return "", fmt.Errorf("解析 relativePath 失败: %w", err)
	}

	// 如果 relativePath 是完整的 URL，直接返回
	if relative.IsAbs() {
		return relative.String(), nil
	}

	// 解析 baseURL
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("解析 baseURL 失败: %w", err)
	}

	// 将相对路径补全为完整路径
	return base.ResolveReference(relative).String(), nil
}
