package websecure

import (
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxBytes     int64  = 5 << 20
	defaultMaxPixels    uint64 = 25_000_000
	defaultMaxRedirects        = 3
)

var (
	ErrInvalidURL       = errors.New("invalid cover URL")
	ErrBlockedAddress   = errors.New("cover URL points to a blocked address")
	ErrFetchFailed      = errors.New("cover download failed")
	ErrTooLarge         = errors.New("cover image is too large")
	ErrUnsupportedImage = errors.New("cover must be a JPEG or PNG image")
	ErrTooManyPixels    = errors.New("cover image has too many pixels")
	ErrStorage          = errors.New("could not store cover image")
)

// CoverFetcher downloads a remote cover while enforcing SSRF, size, image,
// redirect, and timeout limits. Zero-valued limits use conservative defaults.
type CoverFetcher struct {
	AllowHTTP             bool
	MaxBytes              int64
	MaxPixels             uint64
	MaxRedirects          int
	Timeout               time.Duration
	ConnectTimeout        time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration

	lookupIP    func(context.Context, string, string) ([]netip.Addr, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
}

// Fetch downloads rawURL into destDir and returns a randomly named local path.
// The caller owns the returned file.
func (f CoverFetcher) Fetch(ctx context.Context, rawURL, destDir string) (path string, err error) {
	f = f.withDefaults()

	target, err := f.validateURL(rawURL)
	if err != nil {
		return "", err
	}

	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           f.safeDialContext,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   f.TLSHandshakeTimeout,
		ResponseHeaderTimeout: f.ResponseHeaderTimeout,
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   f.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > f.MaxRedirects {
				return ErrFetchFailed
			}
			if _, validateErr := f.validateURL(req.URL.String()); validateErr != nil {
				return validateErr
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", ErrInvalidURL
	}
	req.Header.Set("Accept", "image/jpeg, image/png")

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrInvalidURL) {
			return "", ErrInvalidURL
		}
		if errors.Is(err, ErrBlockedAddress) {
			return "", ErrBlockedAddress
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", ErrFetchFailed
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", ErrFetchFailed
	}
	if resp.ContentLength > f.MaxBytes {
		return "", ErrTooLarge
	}

	file, err := os.CreateTemp(destDir, "cover-*")
	if err != nil {
		return "", ErrStorage
	}
	tempPath := file.Name()
	fileClosed := false
	defer func() {
		if !fileClosed {
			if closeErr := file.Close(); err == nil && closeErr != nil {
				err = ErrStorage
			}
		}
		if err != nil {
			_ = os.Remove(tempPath)
			if path != "" {
				_ = os.Remove(path)
			}
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", ErrStorage
	}

	written, err := io.Copy(file, io.LimitReader(resp.Body, f.MaxBytes+1))
	if err != nil {
		return "", ErrFetchFailed
	}
	if written > f.MaxBytes {
		return "", ErrTooLarge
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", ErrStorage
	}

	header := make([]byte, 512)
	n, readErr := io.ReadFull(file, header)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return "", ErrUnsupportedImage
	}

	var extension string
	switch http.DetectContentType(header[:n]) {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	default:
		return "", ErrUnsupportedImage
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", ErrStorage
	}
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return "", ErrUnsupportedImage
	}
	pixels := uint64(config.Width) * uint64(config.Height)
	if pixels > f.MaxPixels {
		return "", ErrTooManyPixels
	}

	if err := file.Close(); err != nil {
		return "", ErrStorage
	}
	fileClosed = true
	path = tempPath + extension
	if err := os.Rename(tempPath, path); err != nil {
		return "", ErrStorage
	}
	return path, nil
}

func (f CoverFetcher) withDefaults() CoverFetcher {
	if f.MaxBytes <= 0 {
		f.MaxBytes = defaultMaxBytes
	}
	if f.MaxPixels == 0 {
		f.MaxPixels = defaultMaxPixels
	}
	if f.MaxRedirects <= 0 {
		f.MaxRedirects = defaultMaxRedirects
	}
	if f.Timeout <= 0 {
		f.Timeout = 15 * time.Second
	}
	if f.ConnectTimeout <= 0 {
		f.ConnectTimeout = 5 * time.Second
	}
	if f.TLSHandshakeTimeout <= 0 {
		f.TLSHandshakeTimeout = 5 * time.Second
	}
	if f.ResponseHeaderTimeout <= 0 {
		f.ResponseHeaderTimeout = 5 * time.Second
	}
	if f.lookupIP == nil {
		f.lookupIP = net.DefaultResolver.LookupNetIP
	}
	if f.dialContext == nil {
		dialer := &net.Dialer{Timeout: f.ConnectTimeout}
		f.dialContext = dialer.DialContext
	}
	return f
}

func (f CoverFetcher) validateURL(rawURL string) (*url.URL, error) {
	target, err := url.Parse(rawURL)
	if err != nil || target.Host == "" || target.Hostname() == "" {
		return nil, ErrInvalidURL
	}
	if target.User != nil {
		return nil, ErrInvalidURL
	}

	scheme := strings.ToLower(target.Scheme)
	switch scheme {
	case "https":
	case "http":
		if !f.AllowHTTP {
			return nil, ErrInvalidURL
		}
	default:
		return nil, ErrInvalidURL
	}

	port := target.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, ErrInvalidURL
		}
		if scheme == "http" && number != 80 {
			return nil, ErrInvalidURL
		}
		if scheme == "https" && number != 443 {
			return nil, ErrInvalidURL
		}
	}
	return target, nil
}

func (f CoverFetcher) safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrFetchFailed
	}

	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		if isBlockedAddress(literal) {
			return nil, ErrBlockedAddress
		}
		return f.dialContext(ctx, network, net.JoinHostPort(literal.String(), port))
	}

	addresses, err := f.lookupIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrFetchFailed
	}

	for _, addr := range addresses {
		if isBlockedAddress(addr) {
			return nil, ErrBlockedAddress
		}
	}

	for _, addr := range addresses {
		conn, dialErr := f.dialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if dialErr == nil {
			return conn, nil
		}
	}
	return nil, ErrFetchFailed
}

func isBlockedAddress(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" || addr.Is4In6() {
		return true
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	return cgnat.Contains(addr)
}
