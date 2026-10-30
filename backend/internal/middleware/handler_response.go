package middleware

import (
	"errors"
	"net/http"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"backend/internal/security"
)

// clientVisibleCodes 这些错误码的消息由服务端固定文案或参数校验产生，可以安全地返回给客户端。
var clientVisibleCodes = map[int]bool{
	gcode.CodeValidationFailed.Code():     true,
	gcode.CodeInvalidParameter.Code():     true,
	gcode.CodeMissingParameter.Code():     true,
	gcode.CodeInvalidRequest.Code():       true,
	gcode.CodeNotSupported.Code():         true,
	gcode.CodeInvalidConfiguration.Code(): true,
}

// HandlerResponse 替代 ghttp.MiddlewareHandlerResponse：
//   - 成功响应格式不变：{code:0, message:"", data:{...}}；
//   - *security.APIError 按其 HTTP 状态码与业务码输出；
//   - 参数校验类错误返回 400 与校验消息；
//   - 其他错误一律返回 500 与固定文案，绝不回显原始错误文本（可能包含上游响应、内网地址或密钥）。
func HandlerResponse(r *ghttp.Request) {
	r.Middleware.Next()

	if r.Response.BufferLength() > 0 {
		return
	}

	err := r.GetError()
	if err == nil {
		if r.Response.Status > 0 && r.Response.Status != http.StatusOK {
			// 404 等无业务错误的状态，保持 GoFrame 原有格式
			code := gcode.CodeUnknown
			switch r.Response.Status {
			case http.StatusNotFound:
				code = gcode.CodeNotFound
			case http.StatusForbidden:
				code = gcode.CodeNotAuthorized
			}
			writeEnvelope(r, r.Response.Status, code.Code(), http.StatusText(r.Response.Status), nil)
			return
		}
		writeEnvelope(r, http.StatusOK, gcode.CodeOK.Code(), "", r.GetHandlerResponse())
		return
	}

	var apiErr *security.APIError
	if errors.As(err, &apiErr) {
		writeAPIErrorBody(r, apiErr)
		return
	}

	code := gerror.Code(err)
	if clientVisibleCodes[code.Code()] {
		status := http.StatusBadRequest
		if code == gcode.CodeInvalidConfiguration || code == gcode.CodeNotSupported {
			status = http.StatusServiceUnavailable
		}
		msg := err.Error()
		if msg == "" {
			msg = security.ErrInvalidRequest.Message
		}
		writeEnvelope(r, status, code.Code(), msg, g.Map{"request_id": GetRequestID(r)})
		return
	}

	writeAPIErrorBody(r, security.ErrInternal)
}

func writeEnvelope(r *ghttp.Request, status, code int, message string, data interface{}) {
	r.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
	r.Response.WriteHeader(status)
	r.Response.WriteJson(ghttp.DefaultHandlerResponse{Code: code, Message: message, Data: data})
}
