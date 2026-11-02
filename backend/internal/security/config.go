package security

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
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
	// ClientModelAllowedHosts 浏览器自定义模型 Base URL 允许的域名：精确匹配，或 "*.example.com" 匹配其任意子域名（不含 example.com 本身）。
	// 为空时使用 DefaultClientModelAllowedHosts；服务端 llm.models 中 HTTPS baseURL 的域名会在启动时自动追加。
	ClientModelAllowedHosts []string `json:"clientModelAllowedHosts"`
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
		ClientModelAllowedHosts: append([]string(nil), DefaultClientModelAllowedHosts...),
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

// DefaultClientModelAllowedHosts 默认允许的个人模型域名：与前端预设、config.example.yaml 中的官方服务一致。
var DefaultClientModelAllowedHosts = []string{
	"api.openai.com",
	"ark.cn-beijing.volces.com",
	"generativelanguage.googleapis.com",
}

var hostnamePattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizeHostPattern 规范化并校验一条域名白名单规则：转小写、去掉结尾的点；
// 只接受主机名（如 api.example.com）或 "*." 前缀的通配（如 *.example.com，至少包含两级域名）。
// 不接受协议、端口、路径、单独的 "*" 或其它位置的通配符。
func NormalizeHostPattern(p string) (string, error) {
	n := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(p)), ".")
	host := strings.TrimPrefix(n, "*.")
	wildcard := host != n
	if host == "" || len(host) > 253 || !hostnamePattern.MatchString(host) {
		return "", fmt.Errorf("security.clientModelAllowedHosts 规则无效: %q（只能写主机名或 *.域名）", p)
	}
	if wildcard && !strings.Contains(host, ".") {
		return "", fmt.Errorf("security.clientModelAllowedHosts 通配规则过宽: %q（至少写到 *.example.com 这一级）", p)
	}
	return n, nil
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

	// 显式写成空值 / 非正数的字段回退到默认值
	def := DefaultSecurityConfig()
	if cfg.APIKeysEnv == "" {
		cfg.APIKeysEnv = def.APIKeysEnv
	}
	if len(cfg.TrustedOrigins) == 0 {
		cfg.TrustedOrigins = def.TrustedOrigins
	}
	if len(cfg.ClientModelAllowedPorts) == 0 {
		cfg.ClientModelAllowedPorts = def.ClientModelAllowedPorts
	}
	if len(cfg.ClientModelAllowedHosts) == 0 {
		cfg.ClientModelAllowedHosts = append([]string(nil), DefaultClientModelAllowedHosts...)
	}
	for i, h := range cfg.ClientModelAllowedHosts {
		n, err := NormalizeHostPattern(h)
		if err != nil {
			return nil, err
		}
		cfg.ClientModelAllowedHosts[i] = n
	}
	for _, p := range cfg.ClientModelAllowedPorts {
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("security.clientModelAllowedPorts 端口无效: %d", p)
		}
	}
	positiveOr(&cfg.RateLimit.RequestsPerMinute, def.RateLimit.RequestsPerMinute)
	positiveOr(&cfg.RateLimit.Burst, def.RateLimit.Burst)
	positiveOr(&cfg.RateLimit.LimiterIdleMinutes, def.RateLimit.LimiterIdleMinutes)
	positiveOr(&cfg.RateLimit.DailyRequestsPerPrincipal, def.RateLimit.DailyRequestsPerPrincipal)
	positiveOr(&cfg.Concurrency.Global, def.Concurrency.Global)
	positiveOr(&cfg.Concurrency.PerPrincipal, def.Concurrency.PerPrincipal)
	if cfg.Concurrency.QueueTimeoutSeconds < 0 || cfg.Concurrency.QueueTimeoutSeconds > MaxQueueTimeoutSeconds {
		return nil, fmt.Errorf("security.concurrency.queueTimeoutSeconds 必须在 0–%d 之间: %d", MaxQueueTimeoutSeconds, cfg.Concurrency.QueueTimeoutSeconds)
	}

	return cfg, nil
}

// positiveOr 在 *v <= 0 时写入默认值 def。
func positiveOr(v *int, def int) {
	if *v <= 0 {
		*v = def
	}
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
