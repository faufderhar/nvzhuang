package middleware

import (
	"path"
	"strings"
)

// 受保护的业务路由（规范化后的路径）。
// GoFrame 路由会合并多余斜杠（//tryon 与 /tryon 命中同一处理器），
// 因此中间件必须基于规范化路径判断，不能对 r.URL.Path 做精确比较。
var protectedPaths = map[string]bool{
	"/tryon":          true,
	"/comfyui/health": true,
}

// NormalizePath 合并重复斜杠、消解 . / .. 并去掉末尾斜杠，统一转为小写，
// 用于安全判断（不用于路由）。
func NormalizePath(p string) string {
	if p == "" {
		return "/"
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := path.Clean(p) // path.Clean 会合并重复斜杠并去掉末尾斜杠
	return strings.ToLower(cleaned)
}

// IsProtectedPath 判断请求路径（任意写法）是否属于需要鉴权/限流的业务接口。
func IsProtectedPath(p string) bool {
	return protectedPaths[NormalizePath(p)]
}

// IsTryonPath 判断请求路径是否为换装接口。
func IsTryonPath(p string) bool {
	return NormalizePath(p) == "/tryon"
}
