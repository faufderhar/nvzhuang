package cmd

import (
	"net/http"
	"testing"
)

// O-07：升级 GoFrame 后的冒烟检查——接口文档默认不暴露、未知路径返回 404。
// 其余升级回归点（双斜杠路由、413 JSON、错误码与脱敏）由 routes_test.go / errors_test.go 覆盖。
func TestRoutesOpenAPIDisabledByDefault(t *testing.T) {
	base := newRoutesTestServer(t, "openapi-off", true, 3, 1<<20, nil)
	for _, p := range []string{"/api.json", "/swagger", "/swagger/", "/debug/admin"} {
		resp, err := http.Get(base + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s 应返回 404（接口文档/管理页默认关闭），实际 %d", p, resp.StatusCode)
		}
	}
}
