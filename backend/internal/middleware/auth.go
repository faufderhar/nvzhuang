package middleware

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"

	"backend/internal/security"
)

type AuthMiddleware struct {
	tokenStore     *security.TokenStore
	trustedProxies []netip.Prefix
}

func NewAuthMiddleware(store *security.TokenStore) *AuthMiddleware {
	return &AuthMiddleware{tokenStore: store}
}

// NewAuthMiddlewareWithProxies 创建鉴权中间件，并指定可信反向代理（仅这些代理传来的 X-Forwarded-For 会被采信）。
func NewAuthMiddlewareWithProxies(store *security.TokenStore, trustedProxies []netip.Prefix) *AuthMiddleware {
	return &AuthMiddleware{tokenStore: store, trustedProxies: trustedProxies}
}

// Handler 按规范化路径判断是否需要鉴权（//tryon、/tryon/ 等写法同样受保护），可挂在全局路由组上。
func (m *AuthMiddleware) Handler(r *ghttp.Request) {
	if r.Method == http.MethodOptions || !IsProtectedPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}
	m.Enforce(r)
}

// Enforce 无条件执行鉴权，用于直接挂在受保护的路由组上。
func (m *AuthMiddleware) Enforce(r *ghttp.Request) {
	if r.Method == http.MethodOptions {
		r.Middleware.Next()
		return
	}

	authHeader := r.Header.Get("Authorization")
	var token string
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token = strings.TrimSpace(authHeader[7:])
	}

	var principalID string
	if m.tokenStore.IsAuthRequired() {
		if token == "" {
			WriteAPIError(r, security.ErrAuthRequired)
			return
		}
		pid, ok := m.tokenStore.ValidateToken(token)
		if !ok {
			WriteAPIError(r, security.ErrAuthInvalid)
			return
		}
		principalID = pid
	} else {
		// 未开启鉴权模式
		if token != "" {
			if pid, ok := m.tokenStore.ValidateToken(token); ok {
				principalID = pid
			}
		}
		if principalID == "" {
			// 回退到客户端 IP 作为限流标识；不信任客户端自带的 X-Forwarded-For
			principalID = "ip:" + ClientIP(r, m.trustedProxies)
		}
	}
	r.SetCtxVar(CtxPrincipalID, principalID)
	ctx := context.WithValue(r.Context(), CtxPrincipalID, principalID)
	r.SetCtx(ctx)

	r.Middleware.Next()
}
