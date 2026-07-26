package webapp

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

func resolveClientIP(remoteAddr, forwardedFor string, trustedProxies []netip.Prefix) (string, error) {
	remote, err := parseRemoteIP(remoteAddr)
	if err != nil {
		return "", err
	}
	if !prefixContains(trustedProxies, remote) || strings.TrimSpace(forwardedFor) == "" {
		return remote.String(), nil
	}

	parts := strings.Split(forwardedFor, ",")
	hops := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		addr, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			// 代理头只要有一项不合法，就退回到可信 TCP 对端，避免猜测。
			return remote.String(), nil
		}
		hops = append(hops, addr.Unmap())
	}
	for i := len(hops) - 1; i >= 0; i-- {
		if !prefixContains(trustedProxies, hops[i]) {
			return hops[i].String(), nil
		}
	}
	if len(hops) > 0 {
		return hops[0].String(), nil
	}
	return remote.String(), nil
}

func parseRemoteIP(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("解析客户端地址失败: %w", err)
	}
	return addr.Unmap(), nil
}

func prefixContains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
