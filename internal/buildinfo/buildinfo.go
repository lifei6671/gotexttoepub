// Package buildinfo 提供编译时注入的应用版本和构建时间。
package buildinfo

var (
	version   = "dev"
	buildTime = "unknown"
)

// Version 返回当前二进制的版本号。
func Version() string {
	return version
}

// BuildTime 返回当前二进制的 UTC 构建时间。
func BuildTime() string {
	return buildTime
}
