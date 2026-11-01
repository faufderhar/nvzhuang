package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func postTryonRaw(t *testing.T, url, contentType string, body []byte) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/tryon", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer "+routesTestToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// 审计 F10：缺少或错误的 Content-Type 不再落入 GoFrame 表单解析的 500 纯文本，统一返回 400 JSON
func TestRoutesTryonRequiresJSONContentType(t *testing.T) {
	url := newRoutesTestServer(t, "audit-ctype", true, 100, 8<<20, nil)
	body := []byte(`{"person_image":"data:image/png;base64,AAAA","garment_image":"x"}`)
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
		status, resp := postTryonRaw(t, url, ct, body)
		if status != http.StatusBadRequest || !strings.Contains(resp, "INVALID_REQUEST") {
			t.Fatalf("content-type %q: want 400 INVALID_REQUEST JSON, got %d %s", ct, status, resp)
		}
		if strings.Contains(resp, "ParseForm") || strings.Contains(resp, "semicolon") {
			t.Fatalf("content-type %q: framework error leaked: %s", ct, resp)
		}
	}
	// 带 charset 参数的 JSON 仍然接受（进入参数校验，而不是被类型检查拒绝）
	status, resp := postTryonRaw(t, url, "application/json; charset=utf-8", []byte(`{"person_image":"x"}`))
	if status != http.StatusBadRequest || strings.Contains(resp, "INVALID_REQUEST") {
		t.Fatalf("json with charset: want 400 validation (not INVALID_REQUEST), got %d %s", status, resp)
	}
}

// 审计 F4：model / lang / size 先按字符集与长度校验；别名不回显到响应，也不能向日志注入换行
func TestRoutesTryonFieldValidation(t *testing.T) {
	url := newRoutesTestServer(t, "audit-fields", true, 100, 8<<20, nil)
	img := testPNGB64(300, 300)
	cases := []map[string]interface{}{
		{"model": "x\n2026-01-01 [WARN] FAKE line"},
		{"model": "<img src=x onerror=alert(1)>"},
		{"model": strings.Repeat("a", 65)},
		{"lang": "zh\nINJECT"},
		{"size": "1024x1536\nINJECT"},
	}
	for _, extra := range cases {
		payload := map[string]interface{}{"person_image": img, "garment_image": img}
		for k, v := range extra {
			payload[k] = v
		}
		status, body := postTryon(t, url, payload)
		if status != http.StatusBadRequest {
			t.Fatalf("%v: want 400, got %d %s", extra, status, body)
		}
		for _, leak := range []string{"FAKE", "onerror", "INJECT", strings.Repeat("a", 65)} {
			if strings.Contains(body, leak) {
				t.Fatalf("%v: response echoes input %q: %s", extra, leak, body)
			}
		}
	}
}

// 审计 F4：服务端未配置的模型只返回统一文案，不暴露缺 Key / 缺 model 名等配置细节
func TestRoutesUnknownModelGenericMessage(t *testing.T) {
	adapter, err := gcfg.NewAdapterContent(`
llm:
  models:
    nokey:
      provider: openai
      baseURL: "https://api.example.com/v1"
      model: "m"
`)
	if err != nil {
		t.Fatal(err)
	}
	old := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(old)

	url := newRoutesTestServer(t, "audit-model-msg", true, 100, 8<<20, nil)
	img := testPNGB64(300, 300)
	var msgs []string
	for _, alias := range []string{"nokey", "absent-alias"} {
		status, body := postTryon(t, url, map[string]interface{}{"person_image": img, "garment_image": img, "model": alias})
		if status != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d %s", alias, status, body)
		}
		for _, leak := range []string{"API Key", "apiKey", "llm.models", "model 名"} {
			if strings.Contains(body, leak) {
				t.Fatalf("%s: response leaks config detail %q: %s", alias, leak, body)
			}
		}
		var parsed struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal([]byte(body), &parsed)
		msgs = append(msgs, strings.Replace(parsed.Message, alias, "<alias>", 1))
	}
	if msgs[0] != msgs[1] {
		t.Fatalf("configured-without-key and absent aliases must be indistinguishable: %q vs %q", msgs[0], msgs[1])
	}
}

// 审计 F2：同一主体的并发槽位在解析请求体之前占用；已占满时大请求直接 429，不进入 JSON/图片解析
func TestRoutesConcurrencyAcquiredBeforeBodyParse(t *testing.T) {
	url, svc := newRoutesTestServerSvc(t, "audit-gate", true, 100, 8<<20, nil, nil)
	// 主体 ID 与 TokenStore 保持一致：Token SHA-256 的前 8 字节十六进制
	sum := sha256.Sum256([]byte(routesTestToken))
	principal := hex.EncodeToString(sum[:8])
	release, ok := svc.ConcurrencyManager.Acquire(principal)
	if !ok {
		t.Fatal("pre-acquire failed")
	}
	defer release()
	// 非法 JSON：若先解析，会得到 400；先占槽位则应得到 429
	status, body := postTryonRaw(t, url, "application/json", []byte(`{"person_image":`+strings.Repeat("A", 1<<20)))
	if status != http.StatusTooManyRequests || !strings.Contains(body, "CONCURRENCY_LIMITED") {
		t.Fatalf("want 429 CONCURRENCY_LIMITED before parsing, got %d %s", status, body)
	}
}
