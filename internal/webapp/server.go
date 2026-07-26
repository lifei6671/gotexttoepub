package webapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lifei6671/gotexttoepub/internal/jobs"
	"github.com/lifei6671/gotexttoepub/internal/websecure"
)

const clientCookieName = "gte_client"

type coverFetcher interface {
	Fetch(ctx context.Context, rawURL, destDir string) (string, error)
}

// Config 描述 Web HTTP 层所需的依赖和安全边界。
type Config struct {
	Assets         fs.FS
	Manager        *jobs.Manager
	CoverFetcher   coverFetcher
	DataDir        string
	MaxUploadBytes int64
	MaxCoverBytes  int64
	SecureCookie   bool
	PublicOrigin   string
	TrustedProxies []netip.Prefix
	Logger         *slog.Logger
}

// Server 提供嵌入式前端、转换 API 和下载处理。
type Server struct {
	assets         fs.FS
	manager        *jobs.Manager
	coverFetcher   coverFetcher
	incomingDir    string
	maxUploadBytes int64
	maxCoverBytes  int64
	secureCookie   bool
	publicOrigin   string
	trustedProxies []netip.Prefix
	logger         *slog.Logger
	handler        http.Handler

	submissionMu    sync.Mutex
	submissionsByIP map[string]int
	submissionTotal int
	submissionLimit int
}

// NewServer 创建 Web 处理器。
func NewServer(config Config) (*Server, error) {
	if config.Assets == nil {
		return nil, errors.New("前端资源不能为空")
	}
	if config.Manager == nil {
		return nil, errors.New("任务管理器不能为空")
	}
	if config.CoverFetcher == nil {
		return nil, errors.New("封面下载器不能为空")
	}
	if strings.TrimSpace(config.DataDir) == "" {
		return nil, errors.New("数据目录不能为空")
	}
	if config.MaxUploadBytes <= 0 {
		return nil, errors.New("上传大小限制必须大于 0")
	}
	if config.MaxCoverBytes <= 0 {
		return nil, errors.New("封面大小限制必须大于 0")
	}
	publicOrigin, err := normalizeOrigin(config.PublicOrigin)
	if err != nil {
		return nil, fmt.Errorf("公开访问源无效: %w", err)
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{
		assets:          config.Assets,
		manager:         config.Manager,
		coverFetcher:    config.CoverFetcher,
		incomingDir:     filepath.Join(config.DataDir, "incoming"),
		maxUploadBytes:  config.MaxUploadBytes,
		maxCoverBytes:   config.MaxCoverBytes,
		secureCookie:    config.SecureCookie,
		publicOrigin:    publicOrigin,
		trustedProxies:  append([]netip.Prefix(nil), config.TrustedProxies...),
		logger:          logger,
		submissionsByIP: make(map[string]int),
	}
	capacity := config.Manager.Capacity()
	server.submissionLimit = capacity.Workers + capacity.QueueSize
	if err := os.MkdirAll(server.incomingDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建接收目录失败: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleStatic)
	mux.HandleFunc("/api/conversions", server.handleConversions)
	mux.HandleFunc("/api/conversions/", server.handleConversion)
	mux.HandleFunc("/api/capacity", server.handleCapacity)
	mux.HandleFunc("/api/healthz", server.handleHealth)
	server.handler = server.middleware(mux)
	return server, nil
}

// Handler 返回可挂载到 http.Server 的完整处理器。
func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持该请求方法", 0)
		return
	}
	var assetPath string
	switch r.URL.Path {
	case "/":
		assetPath = "index.html"
		_, _ = s.ownerHash(w, r)
	case "/favicon.ico":
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(http.StatusNoContent)
		return
	case "/app.css":
		assetPath = "app.css"
	case "/app.js":
		assetPath = "app.js"
	default:
		http.NotFound(w, r)
		return
	}
	content, err := fs.ReadFile(s.assets, assetPath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "ASSET_ERROR", "页面资源不可用", 0)
		return
	}
	switch filepath.Ext(assetPath) {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(content)
	}
}

func (s *Server) handleConversions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请使用 POST 提交转换", 0)
		return
	}
	if !s.sameOriginRequest(r) {
		writeAPIError(w, http.StatusForbidden, "ORIGIN_REJECTED", "请求来源不受信任", 0)
		return
	}
	ownerHash, err := s.ownerHash(w, r)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "SESSION_ERROR", "无法创建浏览器会话", 0)
		return
	}
	clientIP, err := resolveClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), s.trustedProxies)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "INVALID_CLIENT_IP", "客户端地址无效", 0)
		return
	}
	if err := s.beginSubmission(clientIP); err != nil {
		s.writeManagerError(w, err)
		return
	}
	defer s.endSubmission(clientIP)

	upload, err := parseUpload(w, r, s.incomingDir, s.maxUploadBytes, s.maxCoverBytes)
	if err != nil {
		switch {
		case errors.Is(err, errUploadTooLarge):
			writeAPIError(w, http.StatusRequestEntityTooLarge, "TXT_TOO_LARGE", "TXT 文件超过大小限制", 0)
		case errors.Is(err, errCoverTooLarge):
			writeAPIError(w, http.StatusRequestEntityTooLarge, "COVER_TOO_LARGE", "封面文件超过大小限制", 0)
		case errors.Is(err, errInvalidCover):
			writeAPIError(w, http.StatusBadRequest, "INVALID_COVER_IMAGE", publicUploadMessage(err), 0)
		default:
			writeAPIError(w, http.StatusBadRequest, "INVALID_UPLOAD", publicUploadMessage(err), 0)
		}
		return
	}
	inputPath := upload.InputPath
	coverPath := upload.CoverPath
	defer func() {
		if inputPath != "" {
			_ = os.Remove(inputPath)
		}
		if coverPath != "" {
			_ = os.Remove(coverPath)
		}
	}()

	if upload.CoverURL != "" {
		coverPath, err = s.coverFetcher.Fetch(r.Context(), upload.CoverURL, s.incomingDir)
		if err != nil {
			writeAPIError(w, http.StatusUnprocessableEntity, coverErrorCode(err), "封面链接无法安全使用", 0)
			return
		}
	}

	job, err := s.manager.Submit(r.Context(), jobs.SubmitInput{
		InputPath:    inputPath,
		CoverPath:    coverPath,
		ClientIP:     clientIP,
		OwnerHash:    ownerHash,
		OriginalName: upload.OriginalName,
		InputSize:    upload.InputSize,
	})
	if err != nil {
		s.writeManagerError(w, err)
		return
	}
	// Submit 成功后，文件所有权转交给任务管理器。
	inputPath = ""
	coverPath = ""
	writeJSON(w, http.StatusAccepted, jobResponse(job))
}

func (s *Server) beginSubmission(clientIP string) error {
	if err := s.manager.CanSubmit(clientIP); err != nil {
		return err
	}
	s.submissionMu.Lock()
	defer s.submissionMu.Unlock()
	if s.submissionsByIP[clientIP] > 0 {
		return jobs.ErrIPLimit
	}
	if s.submissionLimit > 0 && s.submissionTotal >= s.submissionLimit {
		return jobs.ErrQueueFull
	}
	s.submissionsByIP[clientIP]++
	s.submissionTotal++
	return nil
}

func (s *Server) endSubmission(clientIP string) {
	s.submissionMu.Lock()
	defer s.submissionMu.Unlock()
	s.submissionsByIP[clientIP]--
	if s.submissionsByIP[clientIP] <= 0 {
		delete(s.submissionsByIP, clientIP)
	}
	if s.submissionTotal > 0 {
		s.submissionTotal--
	}
}

func (s *Server) handleConversion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持该请求方法", 0)
		return
	}
	ownerHash, err := s.ownerHash(w, r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "浏览器会话无效", 0)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/conversions/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || !validJobID(parts[0]) {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 {
		job, err := s.manager.Get(parts[0], ownerHash)
		if err != nil {
			s.writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, jobResponse(job))
		return
	}
	if len(parts) == 2 && parts[1] == "download" {
		s.handleDownload(w, r, parts[0], ownerHash)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request, id, ownerHash string) {
	path, release, err := s.manager.AcquireDownload(id, ownerHash)
	if err != nil {
		s.writeManagerError(w, err)
		return
	}
	defer release()
	job, err := s.manager.Get(id, ownerHash)
	if err != nil {
		s.writeManagerError(w, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeAPIError(w, http.StatusGone, "FILE_GONE", "转换文件已不可用", 0)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeAPIError(w, http.StatusGone, "FILE_GONE", "转换文件已不可用", 0)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": job.OutputName})
	if disposition == "" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "application/epub+zip")
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, job.OutputName, info.ModTime(), file)
}

func (s *Server) handleCapacity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持该请求方法", 0)
		return
	}
	capacity := s.manager.Capacity()
	running := capacity.Active - capacity.Queued
	if running < 0 {
		running = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workers":   capacity.Workers,
		"limit":     capacity.Workers,
		"active":    running,
		"queued":    capacity.Queued,
		"queueSize": capacity.QueueSize,
		"available": running < capacity.Workers && capacity.Queued == 0,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持该请求方法", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ownerHash(w http.ResponseWriter, r *http.Request) (string, error) {
	var ownerID string
	if cookie, err := r.Cookie(clientCookieName); err == nil && validOwnerID(cookie.Value) {
		ownerID = cookie.Value
	} else {
		token := make([]byte, 32)
		if _, err := rand.Read(token); err != nil {
			return "", err
		}
		ownerID = base64.RawURLEncoding.EncodeToString(token)
		http.SetCookie(w, &http.Cookie{
			Name:     clientCookieName,
			Value:    ownerID,
			Path:     "/",
			MaxAge:   int((7 * 24 * time.Hour).Seconds()),
			HttpOnly: true,
			Secure:   s.secureCookie,
			SameSite: http.SameSiteLaxMode,
		})
	}
	hash := sha256.Sum256([]byte(ownerID))
	return hex.EncodeToString(hash[:]), nil
}

func (s *Server) writeManagerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jobs.ErrIPLimit):
		writeAPIError(w, http.StatusTooManyRequests, "IP_LIMIT", "当前 IP 已有任务，请等待完成", 5)
	case errors.Is(err, jobs.ErrQueueFull):
		writeAPIError(w, http.StatusServiceUnavailable, "QUEUE_FULL", "转换队列已满，请稍后重试", 10)
	case errors.Is(err, jobs.ErrStorageFull):
		writeAPIError(w, http.StatusInsufficientStorage, "STORAGE_FULL", "服务器存储空间不足", 60)
	case errors.Is(err, jobs.ErrExpired):
		writeAPIError(w, http.StatusGone, "EXPIRED", "转换文件已过期", 0)
	case errors.Is(err, jobs.ErrNotReady):
		writeAPIError(w, http.StatusConflict, "NOT_READY", "转换文件尚未生成", 3)
	case errors.Is(err, jobs.ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "NOT_FOUND", "任务不存在", 0)
	case errors.Is(err, jobs.ErrNotStarted), errors.Is(err, jobs.ErrClosed):
		writeAPIError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "转换服务暂不可用", 10)
	default:
		s.logger.Error("任务处理失败", "error_type", fmt.Sprintf("%T", err))
		writeAPIError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "服务器处理失败", 0)
	}
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := randomRequestID()
		writer := &statusWriter{ResponseWriter: w}
		writer.Header().Set("X-Request-ID", requestID)
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		started := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("HTTP 处理发生 panic", "request_id", requestID)
				if writer.status == 0 {
					writeAPIError(writer, http.StatusInternalServerError, "INTERNAL_ERROR", "服务器处理失败", 0)
				}
			}
			s.logger.Info("HTTP 请求完成",
				"request_id", requestID,
				"trace_id", requestID,
				"method", r.Method,
				"path", safeLogPath(r.URL.Path),
				"status", writer.statusCode(),
				"response_bytes", writer.bytes,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		}()
		next.ServeHTTP(writer, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += int64(written)
	return written, err
}

func (w *statusWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func safeLogPath(path string) string {
	if strings.HasPrefix(path, "/api/conversions/") {
		if strings.HasSuffix(path, "/download") {
			return "/api/conversions/{id}/download"
		}
		return "/api/conversions/{id}"
	}
	return path
}

type responseJob struct {
	ID            string     `json:"id"`
	OriginalName  string     `json:"originalName"`
	InputSize     int64      `json:"inputSize"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	QueuePosition int        `json:"queuePosition,omitempty"`
	OutputName    string     `json:"outputName,omitempty"`
	OutputSize    int64      `json:"outputSize,omitempty"`
	ErrorCode     string     `json:"errorCode,omitempty"`
}

func jobResponse(job jobs.Job) responseJob {
	return responseJob{
		ID:            job.ID,
		OriginalName:  job.OriginalName,
		InputSize:     job.InputSize,
		Status:        string(job.Status),
		CreatedAt:     job.CreatedAt,
		CompletedAt:   job.CompletedAt,
		ExpiresAt:     job.ExpiresAt,
		QueuePosition: job.QueuePosition,
		OutputName:    job.OutputName,
		OutputSize:    job.OutputSize,
		ErrorCode:     job.ErrorCode,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string, retryAfter int) {
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func coverErrorCode(err error) string {
	switch {
	case errors.Is(err, websecure.ErrTooLarge):
		return "COVER_TOO_LARGE"
	case errors.Is(err, websecure.ErrUnsupportedImage), errors.Is(err, websecure.ErrTooManyPixels):
		return "INVALID_COVER_IMAGE"
	case errors.Is(err, websecure.ErrBlockedAddress):
		return "COVER_ADDRESS_BLOCKED"
	default:
		return "INVALID_COVER_URL"
	}
}

func publicUploadMessage(err error) string {
	message := err.Error()
	if _, after, ok := strings.Cut(message, ": "); ok {
		message = after
	}
	if message == "" {
		return "上传内容无效"
	}
	return message
}

func (s *Server) sameOriginRequest(r *http.Request) bool {
	rawOrigin := strings.TrimSpace(r.Header.Get("Origin"))
	if rawOrigin == "" {
		return true
	}
	origin, err := normalizeOrigin(rawOrigin)
	if err != nil {
		return false
	}
	expected := s.publicOrigin
	if expected == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	return strings.EqualFold(origin, expected)
}

func normalizeOrigin(rawOrigin string) (string, error) {
	rawOrigin = strings.TrimSpace(rawOrigin)
	if rawOrigin == "" {
		return "", nil
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") ||
		origin.Host == "" || origin.User != nil ||
		(origin.Path != "" && origin.Path != "/") ||
		origin.RawQuery != "" || origin.Fragment != "" {
		return "", errors.New("必须是仅包含 http(s) 协议和主机的 Origin")
	}
	return strings.ToLower(origin.Scheme) + "://" + origin.Host, nil
}

func validJobID(value string) bool {
	if len(value) < 20 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func validOwnerID(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func randomRequestID() string {
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return "unavailable"
	}
	return base64.RawURLEncoding.EncodeToString(token)
}
