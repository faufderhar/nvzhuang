package security

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type Service struct {
	Config             *SecurityConfig
	TrustedProxies     []netip.Prefix
	TokenStore         *TokenStore
	RateLimiter        *RateLimiter
	DailyQuotaTracker  *DailyQuotaTracker
	ConcurrencyManager *ConcurrencyManager
}

var defaultService *Service

func InitService(ctx context.Context) (*Service, error) {
	cfg, err := LoadSecurityConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载安全配置失败: %w", err)
	}

	tokenStore, err := NewTokenStore(cfg.AuthRequired, cfg.APIKeysEnv)
	if err != nil {
		return nil, err
	}

	trustedProxies, err := ParseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	if !cfg.AuthRequired {
		g.Log().Warning(ctx, "【安全警告】security.authRequired 设置为 false，已关闭接口访问鉴权，任何客户端均可调用模型！")
	}

	s := &Service{
		Config:             cfg,
		TrustedProxies:     trustedProxies,
		TokenStore:         tokenStore,
		RateLimiter:        NewRateLimiter(cfg.RateLimit.RequestsPerMinute, cfg.RateLimit.Burst, cfg.RateLimit.LimiterIdleMinutes),
		DailyQuotaTracker:  NewDailyQuotaTracker(cfg.RateLimit.DailyRequestsPerPrincipal),
		ConcurrencyManager: NewConcurrencyManager(cfg.Concurrency.Global, cfg.Concurrency.PerPrincipal),
	}
	if dir := strings.TrimSpace(cfg.StateDir); dir != "" {
		store, err := NewBoltQuotaStore(dir, DefaultStateLockTimeout)
		if err != nil {
			// 配置了持久化却无法使用时直接启动失败，避免误以为额度已持久化
			return nil, fmt.Errorf("初始化额度持久化存储失败（security.stateDir）: %w", err)
		}
		s.DailyQuotaTracker.SetStore(store)
		go watchQuotaImport(store)
		g.Log().Info(ctx, "每日额度已启用持久化存储（security.stateDir）")
	} else {
		g.Log().Info(ctx, "每日额度为内存计数，进程重启后清零（如需持久化请设置 security.stateDir）")
	}
	defaultService = s
	return s, nil
}

func GetService() *Service {
	if defaultService == nil {
		cfg := DefaultSecurityConfig()
		store, _ := NewTokenStore(false, "")
		defaultService = &Service{
			Config:             cfg,
			TokenStore:         store,
			RateLimiter:        NewRateLimiter(cfg.RateLimit.RequestsPerMinute, cfg.RateLimit.Burst, cfg.RateLimit.LimiterIdleMinutes),
			DailyQuotaTracker:  NewDailyQuotaTracker(cfg.RateLimit.DailyRequestsPerPrincipal),
			ConcurrencyManager: NewConcurrencyManager(cfg.Concurrency.Global, cfg.Concurrency.PerPrincipal),
		}
	}
	return defaultService
}

// watchQuotaImport 每 30 秒检查一次热备额度快照（quota-import.db），存在则合并，见附录 A。
func watchQuotaImport(store *BoltQuotaStore) {
	mergeOnce := func() {
		n, err := store.MergeImport(time.Now())
		if err != nil {
			g.Log().Warningf(context.Background(), "【额度快照】合并 %s 失败: %v", QuotaImportFile, err)
			return
		}
		if n > 0 {
			g.Log().Infof(context.Background(), "【额度快照】已合并 %d 条额度计数", n)
		}
	}
	mergeOnce()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		mergeOnce()
	}
}
