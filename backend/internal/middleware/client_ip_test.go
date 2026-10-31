package middleware

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

func TestClientIPTrustedProxiesAndUntrustedWarning(t *testing.T) {
	var mu sync.Mutex
	var warned []string
	oldWarn := untrustedProxyWarnFunc
	untrustedProxyWarnFunc = func(_ context.Context, peer netip.Addr) {
		mu.Lock()
		warned = append(warned, peer.String())
		mu.Unlock()
	}
	lastUntrustedProxyWarn.Store(0)
	t.Cleanup(func() { untrustedProxyWarnFunc = oldWarn; lastUntrustedProxyWarn.Store(0) })

	var trusted []netip.Prefix
	var tmu sync.Mutex
	s := g.Server(uniqueServerName("test-client-ip"))
	s.BindHandler("/ip", func(r *ghttp.Request) {
		tmu.Lock()
		p := trusted
		tmu.Unlock()
		r.Response.Write(ClientIP(r, p))
	})
	s.SetPort(0)
	s.SetDumpRouterMap(false)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	url := "http://127.0.0.1:" + strings.Split(s.GetListenedAddress(), ":")[1] + "/ip"
	get := func(xff string) string {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	setTrusted := func(cidrs ...string) {
		tmu.Lock()
		defer tmu.Unlock()
		trusted = nil
		for _, c := range cidrs {
			trusted = append(trusted, netip.MustParsePrefix(c))
		}
	}

	// 可信代理：取最右侧不可信地址，忽略客户端伪造的左侧值
	setTrusted("127.0.0.1/32")
	if got := get("203.0.113.5"); got != "203.0.113.5" {
		t.Fatalf("trusted: %s", got)
	}
	if got := get("198.51.100.9, 203.0.113.5"); got != "203.0.113.5" {
		t.Fatalf("应取最右侧不可信地址: %s", got)
	}
	if got := get("203.0.113.5, 127.0.0.1"); got != "203.0.113.5" {
		t.Fatalf("应跳过可信代理地址: %s", got)
	}
	if len(warned) != 0 {
		t.Fatalf("可信代理不应告警: %v", warned)
	}

	// 未配置可信代理（典型的 Docker 网桥网关误配）：使用对端地址并告警一次
	setTrusted()
	if got := get("203.0.113.5"); got != "127.0.0.1" {
		t.Fatalf("未信任时应使用对端地址: %s", got)
	}
	get("203.0.113.6")
	get("")
	mu.Lock()
	defer mu.Unlock()
	if len(warned) != 1 || warned[0] != "127.0.0.1" {
		t.Fatalf("应只告警一次且只包含对端地址: %v", warned)
	}
}

func TestMaybeWarnUntrustedProxyRateLimited(t *testing.T) {
	n := 0
	oldWarn := untrustedProxyWarnFunc
	untrustedProxyWarnFunc = func(context.Context, netip.Addr) { n++ }
	lastUntrustedProxyWarn.Store(0)
	t.Cleanup(func() { untrustedProxyWarnFunc = oldWarn; lastUntrustedProxyWarn.Store(0) })
	for i := 0; i < 100; i++ {
		maybeWarnUntrustedProxy(context.Background(), netip.MustParseAddr("172.17.0.1"))
	}
	if n != 1 {
		t.Fatalf("告警应限频: %d", n)
	}
	lastUntrustedProxyWarn.Store(1) // 很久以前
	maybeWarnUntrustedProxy(context.Background(), netip.MustParseAddr("172.17.0.1"))
	if n != 2 {
		t.Fatalf("间隔过后应再次告警: %d", n)
	}
}
