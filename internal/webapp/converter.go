package webapp

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lifei6671/gotexttoepub/goepub"
	"github.com/lifei6671/gotexttoepub/internal/jobs"
)

// ConvertEPUB 将任务目录中的受信任输入交给现有转换器，并原子发布最终文件。
func ConvertEPUB(ctx context.Context, _ *jobs.Job, inputPath, coverPath string) (string, int64, error) {
	jobDir := filepath.Dir(inputPath)
	workDir := filepath.Join(jobDir, "work")
	if err := os.RemoveAll(workDir); err != nil {
		return "", 0, fmt.Errorf("清理转换工作目录失败: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", 0, fmt.Errorf("创建转换工作目录失败: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(workDir)
	}()

	book := &goepub.Book{
		Filename: inputPath,
		Cover:    coverPath,
		Output:   workDir,
	}
	if err := goepub.NewEPUBConverter().Convert(ctx, book); err != nil {
		return "", 0, fmt.Errorf("转换 EPUB 失败: %w", err)
	}

	generatedPath, err := book.OutputPath()
	if err != nil {
		return "", 0, fmt.Errorf("定位 EPUB 输出失败: %w", err)
	}
	if !pathWithin(workDir, generatedPath) {
		return "", 0, fmt.Errorf("转换器返回了工作目录之外的文件")
	}
	if err := validateEPUB(generatedPath); err != nil {
		return "", 0, err
	}

	outputName := filepath.Base(generatedPath)
	finalPath := filepath.Join(jobDir, "output.epub")
	if !pathWithin(jobDir, finalPath) {
		return "", 0, fmt.Errorf("EPUB 输出文件名无效")
	}
	if err := os.Remove(finalPath); err != nil && !os.IsNotExist(err) {
		return "", 0, fmt.Errorf("清理旧 EPUB 失败: %w", err)
	}
	if err := os.Rename(generatedPath, finalPath); err != nil {
		return "", 0, fmt.Errorf("发布 EPUB 失败: %w", err)
	}
	if err := os.Chmod(finalPath, 0o600); err != nil {
		_ = os.Remove(finalPath)
		return "", 0, fmt.Errorf("设置 EPUB 权限失败: %w", err)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return "", 0, fmt.Errorf("读取 EPUB 信息失败: %w", err)
	}
	return outputName, info.Size(), nil
}

func validateEPUB(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("生成的 EPUB 不是有效 ZIP: %w", err)
	}
	defer reader.Close()
	if len(reader.File) == 0 {
		return fmt.Errorf("生成的 EPUB 内容为空")
	}
	return nil
}

func pathWithin(base, target string) bool {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
