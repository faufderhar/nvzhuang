package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"

	"backend/internal/security"
)

// BodyLimitMargin 为 GoFrame ClientMaxBodySize 相对业务上限预留的余量，
// 确保超限请求先由本中间件拦截并返回 413 JSON，而不是 GoFrame 的 500 纯文本。
const BodyLimitMargin int64 = 1 << 20

type BodyLimitMiddleware struct {
	maxBytes int64
}

func NewBodyLimitMiddleware(maxBytes int64) *BodyLimitMiddleware {
	if maxBytes <= 0 {
		maxBytes = 25165824 // 24 MiB
	}
	return &BodyLimitMiddleware{maxBytes: maxBytes}
}

// MaxBytes 返回业务请求体上限。
func (m *BodyLimitMiddleware) MaxBytes() int64 { return m.maxBytes }

// Handler 按规范化路径判断是否需要限制请求体，可挂在全局路由组上。
func (m *BodyLimitMiddleware) Handler(r *ghttp.Request) {
	if !IsTryonPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}
	m.Enforce(r)
}

// Enforce 无条件限制 POST 请求体大小，用于直接挂在受保护的路由组上。
func (m *BodyLimitMiddleware) Enforce(r *ghttp.Request) {
	if r.Method != http.MethodPost {
		r.Middleware.Next()
		return
	}

	if r.ContentLength > m.maxBytes {
		WriteAPIError(r, security.ErrRequestTooLarge)
		return
	}

	// 限制读取流，防止 chunked 分块传输或未标明长度的超大请求
	r.Request.Body = http.MaxBytesReader(r.Response.RawWriter(), r.Request.Body, m.maxBytes)

	r.Middleware.Next()

	// 若在后续参数解析阶段触发了 MaxBytesError（GoFrame 会用 gerror 包装），拦截并转换为标准 41301 响应
	if err := r.GetError(); err != nil && IsBodyTooLarge(err) {
		r.SetError(nil)
		r.Response.ClearBuffer()
		WriteAPIError(r, security.ErrRequestTooLarge)
		return
	}
}

// IsBodyTooLarge 判断错误链中是否包含请求体超限错误。
func IsBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return true
	}
	return strings.Contains(err.Error(), "http: request body too large")
}
