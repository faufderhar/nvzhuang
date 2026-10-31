package middleware

import (
	"net/netip"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
)

// ClientIP 返回用于限流/配额的客户端 IP。
// 默认只信任 TCP 连接的对端地址（RemoteAddr），忽略客户端可任意伪造的
// X-Forwarded-For / X-Real-IP 等头；只有当对端地址属于 trustedProxies 时，
// 才从 X-Forwarded-For 自右向左取第一个非可信代理地址（其次 X-Real-IP）。
func ClientIP(r *ghttp.Request, trustedProxies []netip.Prefix) string {
	remote := r.GetRemoteIp()
	remoteAddr, err := netip.ParseAddr(remote)
	if err != nil {
		return remote
	}
	remoteAddr = remoteAddr.Unmap()
	if !inPrefixes(remoteAddr, trustedProxies) {
		return remoteAddr.String()
	}

	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		parts := strings.Split(strings.Join(xff, ","), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			addr, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if err != nil {
				break // 格式异常时停止解析，回退到更可信的地址
			}
			addr = addr.Unmap()
			if !inPrefixes(addr, trustedProxies) {
				return addr.String()
			}
		}
	}
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		if addr, err := netip.ParseAddr(real); err == nil {
			return addr.Unmap().String()
		}
	}
	return remoteAddr.String()
}

func inPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
