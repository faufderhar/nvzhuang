package security

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
)

type SecurityConfig struct {
	AuthRequired        bool     `json:"authRequired"`
	APIKeysEnv          string   `json:"apiKeysEnv"`
	TrustedOrigins      []string `json:"trustedOrigins"`
	AllowPrivateNetwork bool     `json:"allowPrivateNetwork"`
	// TrustedProxies 可信反向代理的 IP 或 CIDR。只有来自这些地址的请求，
	// 才会采信 X-Forwarded-For / X-Real-IP；为空时一律使用 TCP 对端地址。
	TrustedProxies []string `json:"trustedProxies"`
	// ClientModelAllowedPorts 浏览器自定义模型 Base URL 允许使用的 HTTPS 端口，默认仅 443。
	// 仍强制 HTTPS 与公网地址校验，只放宽端口（如部分中转站使用 8443）。
	ClientModelAllowedPorts []int `json:"clientModelAllowedPorts"`
	// StateDir 每日额度持久化目录（bbolt 单文件 quota.db）。为空表示纯内存计数（重启清零）。
	StateDir    string            `json:"stateDir"`
	RateLimit   RateLimitConfig   `json:"rateLimit"`
	Concurrency ConcurrencyConfig `json:"concurrency"`
}

type RateLimitConfig struct {
	RequestsPerMinute         int `json:"requestsPerMinute"`
	Burst                     int `json:"burst"`
	LimiterIdleMinutes        int `json:"limiterIdleMinutes"`
	DailyRequestsPerPrincipal int `json:"dailyRequestsPerPrincipal"`
}

type ConcurrencyConfig struct {
	Global       int `json:"global"`
	PerPrincipal int `json:"perPrincipal"`
	// QueueTimeoutSeconds 全局并发已满时的最长排队秒数；0（默认）表示不排队、立即返回 429。
	// 单主体超限始终立即拒绝。
	QueueTimeoutSeconds int `json:"queueTimeoutSeconds"`
}

// MaxQueueTimeoutSeconds 排队等待上限，避免请求在后端长时间挂起。
const MaxQueueTimeoutSeconds = 60

func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		AuthRequired: true,
		APIKeysEnv:   "TRYON_API_KEYS",
		TrustedOrigins: []string{
			"https://paipiange.github.io",
			"http://localhost:3000",
			"http://127.0.0.1:3000",
		},
		AllowPrivateNetwork:     false,
		ClientModelAllowedPorts: []int{443},
		RateLimit: RateLimitConfig{
			RequestsPerMinute:         10,
			Burst:                     3,
			LimiterIdleMinutes:        30,
			DailyRequestsPerPrincipal: 100,
		},
		Concurrency: ConcurrencyConfig{
			Global:       2,
			PerPrincipal: 1,
		},
	}
}

func LoadSecurityConfig(ctx context.Context) (*SecurityConfig, error) {
	cfg := DefaultSecurityConfig()
	val, err := g.Cfg().Get(ctx, "security")
	if err != nil || val.IsEmpty() {
		return cfg, nil
	}

	if err := val.Scan(cfg); err != nil {
		return nil, err
	}

	if cfg.APIKeysEnv == "" {
		cfg.APIKeysEnv = "TRYON_API_KEYS"
	}
	if len(cfg.TrustedOrigins) == 0 {
		cfg.TrustedOrigins = []string{
			"https://paipiange.github.io",
			"http://localhost:3000",
			"http://127.0.0.1:3000",
		}
	}
	if len(cfg.ClientModelAllowedPorts) == 0 {
		cfg.ClientModelAllowedPorts = []int{443}
	}
	for _, p := range cfg.ClientModelAllowedPorts {
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("security.clientModelAllowedPorts 端口无效: %d", p)
		}
	}
	if cfg.RateLimit.RequestsPerMinute <= 0 {
		cfg.RateLimit.RequestsPerMinute = 10
	}
	if cfg.RateLimit.Burst <= 0 {
		cfg.RateLimit.Burst = 3
	}
	if cfg.RateLimit.LimiterIdleMinutes <= 0 {
		cfg.RateLimit.LimiterIdleMinutes = 30
	}
	if cfg.RateLimit.DailyRequestsPerPrincipal <= 0 {
		cfg.RateLimit.DailyRequestsPerPrincipal = 100
	}
	if cfg.Concurrency.Global <= 0 {
		cfg.Concurrency.Global = 2
	}
	if cfg.Concurrency.PerPrincipal <= 0 {
		cfg.Concurrency.PerPrincipal = 1
	}
	if cfg.Concurrency.QueueTimeoutSeconds < 0 || cfg.Concurrency.QueueTimeoutSeconds > MaxQueueTimeoutSeconds {
		return nil, fmt.Errorf("security.concurrency.queueTimeoutSeconds 必须在 0–%d 之间: %d", MaxQueueTimeoutSeconds, cfg.Concurrency.QueueTimeoutSeconds)
	}

	return cfg, nil
}

// ParseTrustedProxies 解析可信代理列表（支持单个 IP 或 CIDR），格式错误时返回错误以便启动失败。
func ParseTrustedProxies(items []string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			p, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, fmt.Errorf("security.trustedProxies 格式错误: %q", item)
			}
			prefixes = append(prefixes, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("security.trustedProxies 格式错误: %q", item)
		}
		addr = addr.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}
