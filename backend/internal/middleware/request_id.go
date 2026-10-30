package middleware

import (
	"context"

	"github.com/gogf/gf/v2/net/ghttp"
)

const maxRequestIDLength = 64

// SanitizeRequestID 仅接受 1-64 位 [A-Za-z0-9_-] 的请求 ID，否则返回空串。
// 防止日志注入、超长值回显和伪造关联 ID。
func SanitizeRequestID(id string) string {
	if id == "" || len(id) > maxRequestIDLength {
		return ""
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ""
	}
	return id
}

func RequestID(r *ghttp.Request) {
	reqID := SanitizeRequestID(r.Header.Get("X-Request-ID"))
	if reqID == "" {
		reqID = GenerateRequestID()
	}
	r.Response.Header().Set("X-Request-ID", reqID)
	r.SetCtxVar(CtxRequestID, reqID)
	ctx := context.WithValue(r.Context(), CtxRequestID, reqID)
	r.SetCtx(ctx)

	r.Middleware.Next()
}
