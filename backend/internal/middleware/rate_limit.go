package middleware

import (
	"fmt"
	"net/http"

	"github.com/gogf/gf/v2/net/ghttp"

	"backend/internal/security"
)

type RateLimitMiddleware struct {
	limiter *security.RateLimiter
}

func NewRateLimitMiddleware(limiter *security.RateLimiter) *RateLimitMiddleware {
	return &RateLimitMiddleware{limiter: limiter}
}

// Enforce 无条件执行限流，用于直接挂在受保护的路由组上。
func (m *RateLimitMiddleware) Enforce(r *ghttp.Request) {
	if r.Method == http.MethodOptions {
		r.Middleware.Next()
		return
	}

	principalID := r.GetCtxVar(CtxPrincipalID).String()
	if principalID == "" {
		principalID = "anonymous"
	}
	key := principalID
	if !IsTryonPath(r.URL.Path) {
		// 健康检查等辅助接口使用独立的令牌桶，不消耗换装额度
		key = "aux:" + principalID
	}

	ok, retryAfter := m.limiter.Allow(key)
	if !ok {
		r.Response.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		WriteAPIError(r, security.ErrRateLimited)
		return
	}

	r.Middleware.Next()
}
