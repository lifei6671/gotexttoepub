package cmd

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
)

const (
	systemdServiceName  = "gotexttoepub.service"
	managedUnitMarker   = "# Managed by gotexttoepub install"
	defaultBinaryPath   = "/usr/local/bin/gotexttoepub"
	defaultUnitPath     = "/etc/systemd/system/gotexttoepub.service"
	defaultSystemdRun   = "/run/systemd/system"
	systemdCommandLimit = 30 * time.Second
)

//go:embed gotexttoepub.service
var systemdUnitTemplate string

type systemdInstaller struct {
	goos              string
	effectiveUID      func() int
	executable        func() (string, error)
	lookPath          func(string) (string, error)
	runCommand        func(context.Context, string, ...string) error
	binaryPath        string
	unitPath          string
	systemdRuntimeDir string
}

// Install 将当前二进制安装为 systemd 服务。
var Install = newInstallCommand(defaultSystemdInstaller())

func defaultSystemdInstaller() systemdInstaller {
	return systemdInstaller{
		goos:              runtime.GOOS,
		effectiveUID:      currentEffectiveUID,
		executable:        os.Executable,
		lookPath:          findSystemctl,
		runCommand:        runSystemCommand,
		binaryPath:        defaultBinaryPath,
		unitPath:          defaultUnitPath,
		systemdRuntimeDir: defaultSystemdRun,
	}
}

func newInstallCommand(installer systemdInstaller) *cli.Command {
	return &cli.Command{
		Name:        "install",
		Usage:       "安装并启动 systemd 服务",
		Description: "将当前二进制安装到系统目录，写入、启用并启动 gotexttoepub.service。仅支持使用 systemd 的 Linux。",
		Action: func(c *cli.Context) error {
			if err := installer.install(c.Context); err != nil {
				return err
			}
			_, err := fmt.Fprint(c.App.Writer,
				"gotexttoepub.service 已安装并启动\n",
				"查看状态: systemctl status gotexttoepub.service\n",
				"查看日志: journalctl -u gotexttoepub.service -f\n",
			)
			if err != nil {
				return fmt.Errorf("输出安装结果失败: %w", err)
			}
			return nil
		},
	}
}

func (installer systemdInstaller) install(ctx context.Context) error {
	if installer.goos != "linux" {
		return fmt.Errorf("install 命令仅支持 Linux，当前系统为 %s", installer.goos)
	}
	if installer.effectiveUID() != 0 {
		return errors.New("安装 systemd 服务需要 root 权限，请使用 sudo 重新执行")
	}
	info, err := os.Stat(installer.systemdRuntimeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("未检测到正在运行的 systemd")
		}
		return fmt.Errorf("检测 systemd 运行目录失败: %w", err)
	}
	if !info.IsDir() {
		return errors.New("未检测到正在运行的 systemd")
	}
	systemctlPath, err := installer.lookPath("systemctl")
	if err != nil {
		return fmt.Errorf("找不到 systemctl: %w", err)
	}
	if err := installer.runCommand(ctx, systemctlPath, "--version"); err != nil {
		return fmt.Errorf("systemctl 不可用: %w", err)
	}
	if err := ensureManagedUnit(installer.unitPath); err != nil {
		return err
	}

	sourcePath, err := installer.executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}
	sourcePath, err = filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return fmt.Errorf("解析当前可执行文件路径失败: %w", err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("检查当前可执行文件失败: %w", err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return fmt.Errorf("当前可执行文件不是普通文件: %s", sourcePath)
	}

	if err := copyFileAtomic(sourcePath, installer.binaryPath, 0o755); err != nil {
		return fmt.Errorf("安装二进制文件失败: %w", err)
	}
	unitContent, err := renderSystemdUnit(installer.binaryPath)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(installer.unitPath, []byte(unitContent), 0o644); err != nil {
		return fmt.Errorf("安装 systemd unit 失败: %w", err)
	}

	commands := [][]string{
		{"daemon-reload"},
		{"enable", systemdServiceName},
		{"restart", systemdServiceName},
		{"is-active", "--quiet", systemdServiceName},
	}
	for _, args := range commands {
		if err := installer.runCommand(ctx, systemctlPath, args...); err != nil {
			return fmt.Errorf("执行 systemctl %s 失败: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

func ensureManagedUnit(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取现有 systemd unit 失败: %w", err)
	}
	if !strings.HasPrefix(string(content), managedUnitMarker+"\n") {
		return fmt.Errorf("拒绝覆盖非 gotexttoepub 管理的 systemd unit: %s", path)
	}
	return nil
}

func renderSystemdUnit(binaryPath string) (string, error) {
	if !filepath.IsAbs(binaryPath) {
		return "", errors.New("systemd 二进制安装路径必须是绝对路径")
	}
	if strings.ContainsAny(binaryPath, "\r\n") {
		return "", errors.New("systemd 二进制安装路径包含非法换行")
	}
	return strings.ReplaceAll(systemdUnitTemplate, "@BINARY_PATH@", binaryPath), nil
}

func copyFileAtomic(sourcePath, targetPath string, mode fs.FileMode) (returnErr error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer func() {
		if err := source.Close(); returnErr == nil && err != nil {
			returnErr = fmt.Errorf("关闭源文件失败: %w", err)
		}
	}()

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}
	temp, err := os.CreateTemp(targetDir, ".gotexttoepub-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	tempClosed := false
	defer func() {
		if !tempClosed {
			if err := temp.Close(); returnErr == nil && err != nil {
				returnErr = fmt.Errorf("关闭二进制临时文件失败: %w", err)
			}
		}
		if err := os.Remove(tempPath); returnErr == nil && err != nil && !os.IsNotExist(err) {
			returnErr = fmt.Errorf("清理二进制临时文件失败: %w", err)
		}
	}()

	if err := temp.Chmod(mode); err != nil {
		return fmt.Errorf("设置临时文件权限失败: %w", err)
	}
	if _, err := io.Copy(temp, source); err != nil {
		return fmt.Errorf("复制二进制文件失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("同步二进制文件失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭二进制临时文件失败: %w", err)
	}
	tempClosed = true
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("替换二进制文件失败: %w", err)
	}
	return nil
}

func writeFileAtomic(targetPath string, content []byte, mode fs.FileMode) (returnErr error) {
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}
	temp, err := os.CreateTemp(targetDir, ".gotexttoepub-unit-*")
	if err != nil {
		return fmt.Errorf("创建 unit 临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	tempClosed := false
	defer func() {
		if !tempClosed {
			if err := temp.Close(); returnErr == nil && err != nil {
				returnErr = fmt.Errorf("关闭 unit 临时文件失败: %w", err)
			}
		}
		if err := os.Remove(tempPath); returnErr == nil && err != nil && !os.IsNotExist(err) {
			returnErr = fmt.Errorf("清理 unit 临时文件失败: %w", err)
		}
	}()

	if err := temp.Chmod(mode); err != nil {
		return fmt.Errorf("设置 unit 文件权限失败: %w", err)
	}
	if _, err := temp.Write(content); err != nil {
		return fmt.Errorf("写入 unit 文件失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("同步 unit 文件失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭 unit 临时文件失败: %w", err)
	}
	tempClosed = true
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("替换 unit 文件失败: %w", err)
	}
	return nil
}

func findSystemctl(string) (string, error) {
	for _, path := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func runSystemCommand(ctx context.Context, name string, args ...string) error {
	commandContext, cancel := context.WithTimeout(ctx, systemdCommandLimit)
	defer cancel()

	output, err := exec.CommandContext(commandContext, name, args...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}
