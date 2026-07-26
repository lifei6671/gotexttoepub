package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/lifei6671/gotexttoepub/internal/jobs"
	"github.com/lifei6671/gotexttoepub/internal/webapp"
	"github.com/lifei6671/gotexttoepub/internal/websecure"
	webassets "github.com/lifei6671/gotexttoepub/web"
)

const (
	defaultDiskQuotaBytes = int64(5 * 1024 * 1024 * 1024)
	defaultMaxUploadBytes = int64(20 * 1024 * 1024)
	defaultMaxCoverBytes  = int64(5 * 1024 * 1024)
)

// Serve 是内嵌 Web 转换服务的命令入口。
var Serve = newServeCommand()

func newServeCommand() *cli.Command {
	return &cli.Command{
		Name:        "serve",
		Usage:       "启动 TXT 转 EPUB Web 服务",
		Description: "启动内嵌网页、受限转换队列和 24 小时文件清理服务。",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "address", Value: "127.0.0.1:8080", Usage: "HTTP 监听地址", EnvVars: []string{"GTE_WEB_ADDR"}},
			&cli.StringFlag{Name: "data-dir", Value: "./data", Usage: "任务和 EPUB 数据目录", EnvVars: []string{"GTE_DATA_DIR"}},
			&cli.IntFlag{Name: "workers", Value: 2, Usage: "全局同时转换数", EnvVars: []string{"GTE_GLOBAL_CONCURRENCY"}},
			&cli.IntFlag{Name: "queue-capacity", Value: 8, Usage: "全局等待队列容量", EnvVars: []string{"GTE_QUEUE_CAPACITY"}},
			&cli.IntFlag{Name: "per-ip-limit", Value: 1, Usage: "每个 IP 允许的未完成任务数", EnvVars: []string{"GTE_PER_IP_CONCURRENCY"}},
			&cli.DurationFlag{Name: "result-ttl", Value: 24 * time.Hour, Usage: "转换文件保留时间", EnvVars: []string{"GTE_RESULT_TTL"}},
			&cli.DurationFlag{Name: "cleanup-interval", Value: 5 * time.Minute, Usage: "过期和配额清理间隔", EnvVars: []string{"GTE_CLEANUP_INTERVAL"}},
			&cli.DurationFlag{Name: "job-timeout", Value: 10 * time.Minute, Usage: "单次转换超时", EnvVars: []string{"GTE_JOB_TIMEOUT"}},
			&cli.Int64Flag{Name: "disk-quota-bytes", Value: defaultDiskQuotaBytes, Usage: "数据目录最大占用字节数", EnvVars: []string{"GTE_DISK_QUOTA_BYTES"}},
			&cli.Int64Flag{Name: "max-upload-bytes", Value: defaultMaxUploadBytes, Usage: "单个 TXT 最大字节数", EnvVars: []string{"GTE_MAX_UPLOAD_BYTES"}},
			&cli.Int64Flag{Name: "max-cover-bytes", Value: defaultMaxCoverBytes, Usage: "上传或远程封面最大字节数", EnvVars: []string{"GTE_MAX_COVER_BYTES"}},
			&cli.StringSliceFlag{Name: "trusted-proxy", Usage: "可信反向代理 CIDR，可重复设置", EnvVars: []string{"GTE_TRUSTED_PROXY_CIDRS"}},
			&cli.BoolFlag{Name: "secure-cookie", Usage: "仅通过 HTTPS 发送浏览器任务 Cookie", EnvVars: []string{"GTE_SECURE_COOKIE"}},
			&cli.StringFlag{Name: "public-origin", Usage: "公开访问 Origin，例如 https://epub.example.com", EnvVars: []string{"GTE_PUBLIC_ORIGIN"}},
		},
		Action: runServe,
	}
}

func runServe(c *cli.Context) error {
	if err := validateServeFlags(c); err != nil {
		return err
	}
	dataDir, err := filepath.Abs(c.String("data-dir"))
	if err != nil {
		return fmt.Errorf("解析数据目录失败: %w", err)
	}
	trustedProxies, err := parseTrustedProxies(c.StringSlice("trusted-proxy"))
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if !c.Bool("secure-cookie") && !isLoopbackAddress(c.String("address")) {
		logger.Warn("服务监听在非本机地址但未启用 Secure Cookie，请仅在受控开发网络使用")
	}
	// 现有转换库的标准日志包含书名和章节标题；Web 模式统一关闭这类内容日志。
	log.SetOutput(io.Discard)

	manager, err := jobs.NewManager(jobs.Config{
		DataDir:         dataDir,
		Workers:         c.Int("workers"),
		QueueSize:       c.Int("queue-capacity"),
		PerIPLimit:      c.Int("per-ip-limit"),
		Retention:       c.Duration("result-ttl"),
		JobTimeout:      c.Duration("job-timeout"),
		CleanupInterval: c.Duration("cleanup-interval"),
		MaxDiskBytes:    c.Int64("disk-quota-bytes"),
		Convert:         webapp.ConvertEPUB,
	})
	if err != nil {
		return fmt.Errorf("初始化任务管理器失败: %w", err)
	}

	signalContext, stopSignals := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if err := manager.Start(signalContext); err != nil {
		return fmt.Errorf("启动任务管理器失败: %w", err)
	}
	defer func() {
		if err := manager.Close(); err != nil {
			logger.Error("关闭任务管理器失败", "error", err)
		}
	}()

	app, err := webapp.NewServer(webapp.Config{
		Assets:  webassets.Assets,
		Manager: manager,
		CoverFetcher: websecure.CoverFetcher{
			MaxBytes:     c.Int64("max-cover-bytes"),
			MaxPixels:    25_000_000,
			MaxRedirects: 3,
			Timeout:      15 * time.Second,
		},
		DataDir:        dataDir,
		MaxUploadBytes: c.Int64("max-upload-bytes"),
		MaxCoverBytes:  c.Int64("max-cover-bytes"),
		SecureCookie:   c.Bool("secure-cookie"),
		PublicOrigin:   c.String("public-origin"),
		TrustedProxies: trustedProxies,
		Logger:         logger,
	})
	if err != nil {
		return fmt.Errorf("初始化 Web 服务失败: %w", err)
	}

	httpServer := &http.Server{
		Addr:              c.String("address"),
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	listener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", httpServer.Addr, err)
	}
	logger.Info("Web 服务已启动", "address", listener.Addr().String(), "data_dir", dataDir)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.Serve(listener)
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("Web 服务异常退出: %w", err)
		}
		return nil
	case <-signalContext.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("优雅关闭 Web 服务失败: %w", err)
	}
	logger.Info("Web 服务已停止")
	return nil
}

func validateServeFlags(c *cli.Context) error {
	switch {
	case c.Int("workers") <= 0:
		return errors.New("--workers 必须大于 0")
	case c.Int("queue-capacity") <= 0:
		return errors.New("--queue-capacity 必须大于 0")
	case c.Int("per-ip-limit") <= 0:
		return errors.New("--per-ip-limit 必须大于 0")
	case c.Duration("result-ttl") <= 0:
		return errors.New("--result-ttl 必须大于 0")
	case c.Duration("cleanup-interval") <= 0:
		return errors.New("--cleanup-interval 必须大于 0")
	case c.Duration("job-timeout") <= 0:
		return errors.New("--job-timeout 必须大于 0")
	case c.Int64("disk-quota-bytes") <= 0:
		return errors.New("--disk-quota-bytes 必须大于 0")
	case c.Int64("max-upload-bytes") <= 0:
		return errors.New("--max-upload-bytes 必须大于 0")
	case c.Int64("max-cover-bytes") <= 0:
		return errors.New("--max-cover-bytes 必须大于 0")
	case c.Int64("disk-quota-bytes") <= c.Int64("max-upload-bytes")+c.Int64("max-cover-bytes"):
		return errors.New("--disk-quota-bytes 必须大于单次上传和封面限制之和")
	default:
		return nil
	}
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func parseTrustedProxies(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("可信代理 CIDR 无效 %q: %w", value, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}
