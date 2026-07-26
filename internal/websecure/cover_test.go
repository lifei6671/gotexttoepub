package websecure

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFetchRejectsInvalidURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rawURL    string
		allowHTTP bool
	}{
		{name: "FTP", rawURL: "ftp://cover.test/book.jpg", allowHTTP: true},
		{name: "HTTP disabled", rawURL: "http://cover.test/book.jpg"},
		{name: "userinfo", rawURL: "https://user:pass@cover.test/book.jpg"},
		{name: "HTTP nonstandard port", rawURL: "http://cover.test:8080/book.jpg", allowHTTP: true},
		{name: "uppercase HTTP nonstandard port", rawURL: "HTTP://cover.test:8080/book.jpg", allowHTTP: true},
		{name: "HTTPS nonstandard port", rawURL: "https://cover.test:8443/book.jpg"},
		{name: "missing host", rawURL: "https:///book.jpg"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fetcher := CoverFetcher{AllowHTTP: test.allowHTTP}
			_, err := fetcher.Fetch(context.Background(), test.rawURL, t.TempDir())
			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("Fetch() error = %v, want ErrInvalidURL", err)
			}
		})
	}
}

func TestFetchRejectsBlockedResolvedAddresses(t *testing.T) {
	t.Parallel()

	tests := []string{
		"127.0.0.1",
		"10.0.0.1",
		"169.254.1.1",
		"224.0.0.1",
		"0.0.0.0",
		"100.64.0.1",
		"::1",
		"fe80::1",
		"ff02::1",
		"::ffff:192.0.2.1",
	}

	for _, rawIP := range tests {
		t.Run(rawIP, func(t *testing.T) {
			t.Parallel()
			fetcher := CoverFetcher{
				AllowHTTP: true,
				lookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr(rawIP)}, nil
				},
				dialContext: func(context.Context, string, string) (net.Conn, error) {
					t.Fatal("blocked address reached dialer")
					return nil, nil
				},
			}
			_, err := fetcher.Fetch(context.Background(), "http://cover.test/book.png", t.TempDir())
			if !errors.Is(err, ErrBlockedAddress) {
				t.Fatalf("Fetch() error = %v, want ErrBlockedAddress", err)
			}
		})
	}
}

func TestFetchValidatesRedirectTarget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/secret", http.StatusFound)
	}))
	defer server.Close()

	fetcher := testFetcher(t, server)
	_, err := fetcher.Fetch(context.Background(), "http://cover.test/start", t.TempDir())
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("Fetch() error = %v, want ErrBlockedAddress", err)
	}
}

func TestFetchLimitsRedirects(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step := strings.TrimPrefix(r.URL.Path, "/")
		switch step {
		case "start":
			http.Redirect(w, r, "http://cover.test/one", http.StatusFound)
		case "one":
			http.Redirect(w, r, "http://cover.test/two", http.StatusFound)
		default:
			http.Redirect(w, r, "http://cover.test/three", http.StatusFound)
		}
	}))
	defer server.Close()

	fetcher := testFetcher(t, server)
	fetcher.MaxRedirects = 2
	_, err := fetcher.Fetch(context.Background(), "http://cover.test/start", t.TempDir())
	if !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("Fetch() error = %v, want ErrFetchFailed", err)
	}
}

func TestFetchRejectsOversizedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.Handler
	}{
		{
			name: "content length",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "9")
				_, _ = w.Write(bytes.Repeat([]byte("x"), 9))
			}),
		},
		{
			name: "stream limit",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.(http.Flusher).Flush()
				_, _ = w.Write(bytes.Repeat([]byte("x"), 9))
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(test.handler)
			defer server.Close()

			fetcher := testFetcher(t, server)
			fetcher.MaxBytes = 8
			_, err := fetcher.Fetch(context.Background(), "http://cover.test/book", t.TempDir())
			if !errors.Is(err, ErrTooLarge) {
				t.Fatalf("Fetch() error = %v, want ErrTooLarge", err)
			}
		})
	}
}

func TestFetchRejectsUnsupportedAndExcessivePixelImages(t *testing.T) {
	t.Parallel()

	largePNG := encodePNG(t, 11, 11)
	tests := []struct {
		name      string
		body      []byte
		maxPixels uint64
		want      error
	}{
		{name: "plain text", body: []byte("not an image"), want: ErrUnsupportedImage},
		{name: "too many pixels", body: largePNG, maxPixels: 100, want: ErrTooManyPixels},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(test.body)
			}))
			defer server.Close()

			fetcher := testFetcher(t, server)
			fetcher.MaxPixels = test.maxPixels
			_, err := fetcher.Fetch(context.Background(), "http://cover.test/book", t.TempDir())
			if !errors.Is(err, test.want) {
				t.Fatalf("Fetch() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestFetchStoresValidPNGWithRestrictedPermissions(t *testing.T) {
	t.Parallel()

	body := encodePNG(t, 2, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	destDir := t.TempDir()
	fetcher := testFetcher(t, server)
	path, err := fetcher.Fetch(context.Background(), "http://cover.test/book", destDir)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if filepath.Dir(path) != destDir || filepath.Ext(path) != ".png" {
		t.Fatalf("Fetch() path = %q, want random PNG in %q", path, destDir)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("stored cover differs from response body")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows ACLs do not expose Unix permission bits through FileMode.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("cover permissions = %o, want 600", info.Mode().Perm())
	}
}

func testFetcher(t *testing.T, server *httptest.Server) CoverFetcher {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return CoverFetcher{
		AllowHTTP: true,
		lookupIP: func(_ context.Context, _, _ string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("203.0.113.10")}, nil
		},
		dialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, serverURL.Host)
		},
	}
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.White)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
