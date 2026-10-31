package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"backend/internal/security"
)

const CtxRequestID = "request_id"
const CtxPrincipalID = "principal_id"

func GetRequestID(r *ghttp.Request) string {
	if val := r.GetCtxVar(CtxRequestID); !val.IsEmpty() {
		return val.String()
	}
	return SanitizeRequestID(r.Header.Get("X-Request-ID"))
}

func GenerateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x%s", time.Now().UnixNano(), hex.EncodeToString(b))
}

// WriteAPIError 输出标准错误响应并终止后续处理。
func WriteAPIError(r *ghttp.Request, err *security.APIError) {
	writeAPIErrorBody(r, err)
	r.ExitAll()
}

func writeAPIErrorBody(r *ghttp.Request, err *security.APIError) {
	reqID := GetRequestID(r)
	r.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
	if reqID != "" {
		r.Response.Header().Set("X-Request-ID", reqID)
	}
	r.Response.WriteHeader(err.HTTPStatus)
	r.Response.WriteJson(g.Map{
		"code":    err.BizCode,
		"message": err.Message,
		"data": g.Map{
			"error_code": err.ErrorCode,
			"request_id": reqID,
		},
	})
}
