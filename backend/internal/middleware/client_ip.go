package middleware

import (
	"context"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gogf/gf/v2/frame/g"
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
		if (remoteAddr.IsPrivate() || remoteAddr.IsLoopback()) &&
			(r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "") {
			maybeWarnUntrustedProxy(r.Context(), remoteAddr)
		}
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

// untrustedProxyWarnInterval 同一进程内告警的最小间隔，避免被请求刷屏。
const untrustedProxyWarnInterval = 10 * time.Minute

var (
	lastUntrustedProxyWarn atomic.Int64
	// untrustedProxyWarnFunc 便于测试替换；只记录对端地址，绝不记录转发头内容。
	untrustedProxyWarnFunc = func(ctx context.Context, peer netip.Addr) {
		g.Log().Warningf(ctx, "【部署告警】收到来自私有地址 %s 的 X-Forwarded-For/X-Real-IP，但该地址不在 security.trustedProxies 中，"+
			"限流与额度将按该代理地址统计（所有用户共享同一额度）。若这是你的反向代理，请把它加入 trustedProxies"+
			"（Docker 部署注意：宿主机 Nginx 访问发布端口时对端通常是 Docker 网桥网关，而不是 127.0.0.1）", peer)
	}
)

func maybeWarnUntrustedProxy(ctx context.Context, peer netip.Addr) {
	now := time.Now().UnixNano()
	last := lastUntrustedProxyWarn.Load()
	if last != 0 && time.Duration(now-last) < untrustedProxyWarnInterval {
		return
	}
	if lastUntrustedProxyWarn.CompareAndSwap(last, now) {
		untrustedProxyWarnFunc(ctx, peer)
	}
}
