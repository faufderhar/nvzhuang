package middleware

import (
	"path"
	"strings"
)

// GoFrame 路由会合并多余斜杠（//tryon 与 /tryon 命中同一处理器），
// 因此按路径做安全判断时必须先规范化，不能对 r.URL.Path 做精确比较。

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

// IsTryonPath 判断请求路径是否为换装接口。
func IsTryonPath(p string) bool {
	return NormalizePath(p) == "/tryon"
}
