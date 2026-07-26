package webapp

import (
	"net/netip"
	"testing"
)

func TestResolveClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	tests := []struct {
		name      string
		remote    string
		forwarded string
		trusted   []netip.Prefix
		want      string
	}{
		{name: "直连忽略伪造头", remote: "203.0.113.9:1234", forwarded: "198.51.100.1", trusted: trusted, want: "203.0.113.9"},
		{name: "可信单层代理", remote: "10.0.0.2:443", forwarded: "198.51.100.7", trusted: trusted, want: "198.51.100.7"},
		{name: "从右剥离可信代理", remote: "10.0.0.2:443", forwarded: "198.51.100.8, 10.1.2.3", trusted: trusted, want: "198.51.100.8"},
		{name: "非法代理头退回对端", remote: "10.0.0.2:443", forwarded: "not-an-ip", trusted: trusted, want: "10.0.0.2"},
		{name: "规范化映射地址", remote: "[::ffff:192.0.2.4]:80", want: "192.0.2.4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveClientIP(tt.remote, tt.forwarded, tt.trusted)
			if err != nil {
				t.Fatalf("resolveClientIP() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveClientIPRejectsInvalidRemote(t *testing.T) {
	if _, err := resolveClientIP("invalid", "", nil); err == nil {
		t.Fatal("expected invalid remote address to fail")
	}
}
