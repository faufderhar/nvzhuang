package middleware

import (
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"

	"backend/internal/security"
)

// CtxConcurrencyHeld 标记本请求已由 TryonGate 占用并发槽位，控制器不再重复占用。
const CtxConcurrencyHeld = "tryon_concurrency_held"

// ConcurrencyRetryAfterSeconds 并发已满（含排队超时）时建议客户端的重试间隔。
const ConcurrencyRetryAfterSeconds = 5

// TryonGate 在 GoFrame 解析请求体（JSON → 结构体 → 图片解码）之前完成两件事：
//  1. 只接受 application/json：其它类型会被 GoFrame 按表单解析，失败时返回 500 纯文本并暴露框架内部错误；
//  2. 先占用并发槽位：解析一张 20 MB 级的 Data URL 需要数倍内存，若先解析再排队，
//     同一 Token 的多个并发大请求会在被并发限制拒绝之前就把内存放大到数百 MB。
//
// 必须挂在 Auth、RateLimit 之后（需要主体 ID，且被限流的请求不应占用槽位）。
type TryonGate struct {
	svc *security.Service
}

func NewTryonGate(svc *security.Service) *TryonGate { return &TryonGate{svc: svc} }

func (m *TryonGate) Enforce(r *ghttp.Request) {
	if r.Method != http.MethodPost || !IsTryonPath(r.URL.Path) {
		r.Middleware.Next()
		return
	}
	if !IsJSONContentType(r.Header.Get("Content-Type")) {
		WriteAPIError(r, security.ErrInvalidRequest)
		return
	}

	principalID := r.GetCtxVar(CtxPrincipalID).String()
	if principalID == "" {
		principalID = "anonymous"
	}
	// 每次请求读取当前服务实例的字段（测试会临时替换 ConcurrencyManager）
	wait := time.Duration(m.svc.Config.Concurrency.QueueTimeoutSeconds) * time.Second
	release, ok := m.svc.ConcurrencyManager.AcquireWait(r.Context(), principalID, wait)
	if !ok {
		r.Response.Header().Set("Retry-After", strconv.Itoa(ConcurrencyRetryAfterSeconds))
		WriteAPIError(r, security.ErrConcurrencyLimited)
		return
	}
	defer release()
	r.SetCtxVar(CtxConcurrencyHeld, true)
	r.Middleware.Next()
}

// IsJSONContentType 判断 Content-Type 是否为 application/json（允许带 charset 等参数）。
func IsJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && mt == "application/json"
}
