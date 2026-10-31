package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/frame/g"

	"backend/internal/security"
)

const routesTestToken = "routes-test-token-0123456789abcdefghijklmnop"

type apiErrBody struct {
	Code int `json:"code"`
	Data struct {
		ErrorCode string `json:"error_code"`
	} `json:"data"`
}

func newRoutesTestServer(t *testing.T, name string, authRequired bool, burst int, maxBody int64, trustedProxies []string) string {
	t.Helper()
	envKey := "TEST_ROUTES_KEYS_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	os.Setenv(envKey, routesTestToken)
	t.Cleanup(func() { os.Unsetenv(envKey) })

	store, err := security.NewTokenStore(authRequired, envKey)
	if err != nil {
		t.Fatal(err)
	}
	proxies, err := security.ParseTrustedProxies(trustedProxies)
	if err != nil {
		t.Fatal(err)
	}
	cfg := security.DefaultSecurityConfig()
	svc := &security.Service{
		Config:             cfg,
		TrustedProxies:     proxies,
		TokenStore:         store,
		RateLimiter:        security.NewRateLimiter(1, burst, 10),
		DailyQuotaTracker:  security.NewDailyQuotaTracker(100),
		ConcurrencyManager: security.NewConcurrencyManager(2, 1),
	}

	s := g.Server(uniqueServerName(name))
	RegisterRoutes(s, svc, maxBody)
	s.SetPort(0)
	s.SetDumpRouterMap(false)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	return "http://127.0.0.1:" + strings.Split(s.GetListenedAddress(), ":")[1]
}

func doReq(t *testing.T, method, url string, body io.Reader, headers map[string]string) (int, apiErrBody) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed apiErrBody
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed
}

// F-01：任何能命中处理器的路径写法都必须经过鉴权
func TestRoutesPathVariantsRequireAuth(t *testing.T) {
	base := newRoutesTestServer(t, "routes-auth", true, 10, 4096, nil)
	payload := `{"person_image":"x","garment_image":"y"}`
	for _, p := range []string{"/tryon", "//tryon", "///tryon", "//tryon/", "/tryon/", "/./tryon"} {
		status, body := doReq(t, http.MethodPost, base+p, strings.NewReader(payload), map[string]string{"Content-Type": "application/json"})
		if status == http.StatusNotFound {
			continue // 路由未命中，不构成绕过
		}
		if status != http.StatusUnauthorized || body.Code != 40101 {
			t.Fatalf("path %q: expected 401/40101, got %d %+v", p, status, body)
		}
	}
	for _, p := range []string{"/comfyui/health", "//comfyui/health", "/comfyui//health/"} {
		status, body := doReq(t, http.MethodGet, base+p, nil, nil)
		if status == http.StatusNotFound {
			continue
		}
		if status != http.StatusUnauthorized || body.Code != 40101 {
			t.Fatalf("path %q: expected 401/40101, got %d %+v", p, status, body)
		}
	}
	// 公共接口不受影响
	if status, _ := doReq(t, http.MethodGet, base+"/hello", nil, nil); status != http.StatusOK {
		t.Fatalf("/hello expected 200, got %d", status)
	}
	// 携带有效 Token 时可进入处理器（图片无效 → 422，而不是 401）
	status, body := doReq(t, http.MethodPost, base+"//tryon", strings.NewReader(payload), map[string]string{
		"Content-Type": "application/json", "Authorization": "Bearer " + routesTestToken,
	})
	if status == http.StatusUnauthorized {
		t.Fatalf("valid token rejected: %d %+v", status, body)
	}
}

// F-01 + F-11：请求体上限对路径变体同样生效，且返回 413 JSON
func TestRoutesBodyLimitReturns413(t *testing.T) {
	base := newRoutesTestServer(t, "routes-body", true, 10, 2048, nil)
	big := `{"person_image":"` + strings.Repeat("A", 4096) + `","garment_image":"y"}`
	headers := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + routesTestToken}
	for _, p := range []string{"/tryon", "//tryon", "//tryon/"} {
		status, body := doReq(t, http.MethodPost, base+p, strings.NewReader(big), headers)
		if status != http.StatusRequestEntityTooLarge || body.Code != 41301 || body.Data.ErrorCode != "REQUEST_TOO_LARGE" {
			t.Fatalf("path %q: expected 413/41301, got %d %+v", p, status, body)
		}
	}
	// 未声明 Content-Length 的分块请求同样返回 413 JSON
	status, body := doReq(t, http.MethodPost, base+"/tryon", io.MultiReader(strings.NewReader(big)), headers)
	if status != http.StatusRequestEntityTooLarge || body.Code != 41301 {
		t.Fatalf("chunked: expected 413/41301, got %d %+v", status, body)
	}
}

// F-11：业务上限大于 GoFrame 默认 8MB 时，9MiB 请求应进入业务校验，而不是 500
func TestRoutesClientMaxBodySizeRaised(t *testing.T) {
	const limit = 12 << 20
	base := newRoutesTestServer(t, "routes-body-big", true, 10, limit, nil)
	headers := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + routesTestToken}
	payload := `{"person_image":"` + strings.Repeat("A", 9<<20) + `","garment_image":"y"}`
	status, body := doReq(t, http.MethodPost, base+"/tryon", strings.NewReader(payload), headers)
	if status == http.StatusInternalServerError || status == http.StatusRequestEntityTooLarge {
		t.Fatalf("9MiB body within limit should reach validation, got %d %+v", status, body)
	}
	over := `{"person_image":"` + strings.Repeat("A", 13<<20) + `","garment_image":"y"}`
	status, body = doReq(t, http.MethodPost, base+"/tryon", io.MultiReader(strings.NewReader(over)), headers)
	if status != http.StatusRequestEntityTooLarge || body.Code != 41301 {
		t.Fatalf("13MiB chunked body: expected 413/41301, got %d %+v", status, body)
	}
}

// F-01 + F-04：未开启鉴权时，路径变体与伪造 X-Forwarded-For 都不能绕过限流
func TestRoutesRateLimitIgnoresSpoofedXFF(t *testing.T) {
	base := newRoutesTestServer(t, "routes-xff", false, 1, 4096, nil)
	payload := `{"person_image":"x","garment_image":"y"}`
	status, _ := doReq(t, http.MethodPost, base+"/tryon", strings.NewReader(payload), map[string]string{
		"Content-Type": "application/json", "X-Forwarded-For": "1.2.3.0",
	})
	if status == http.StatusTooManyRequests {
		t.Fatalf("first request should not be limited")
	}
	for i, p := range []string{"//tryon", "/tryon", "///tryon/"} {
		status, body := doReq(t, http.MethodPost, base+p, strings.NewReader(payload), map[string]string{
			"Content-Type": "application/json", "X-Forwarded-For": "1.2.3." + string(rune('1'+i)), "X-Real-IP": "5.6.7.8",
		})
		if status != http.StatusTooManyRequests || body.Code != 42901 {
			t.Fatalf("request %d (%s) with spoofed XFF: expected 429, got %d %+v", i, p, status, body)
		}
	}
}

// F-04：来自可信代理时采信 X-Forwarded-For
func TestRoutesTrustedProxyXFF(t *testing.T) {
	base := newRoutesTestServer(t, "routes-xff-trusted", false, 1, 4096, []string{"127.0.0.1"})
	payload := `{"person_image":"x","garment_image":"y"}`
	for _, ip := range []string{"203.0.113.1", "203.0.113.2"} {
		status, body := doReq(t, http.MethodPost, base+"/tryon", strings.NewReader(payload), map[string]string{
			"Content-Type": "application/json", "X-Forwarded-For": ip,
		})
		if status == http.StatusTooManyRequests {
			t.Fatalf("distinct client %s behind trusted proxy should have its own bucket: %+v", ip, body)
		}
	}
	status, _ := doReq(t, http.MethodPost, base+"/tryon", strings.NewReader(payload), map[string]string{
		"Content-Type": "application/json", "X-Forwarded-For": "203.0.113.1",
	})
	if status != http.StatusTooManyRequests {
		t.Fatalf("same client behind trusted proxy should be limited, got %d", status)
	}
}
