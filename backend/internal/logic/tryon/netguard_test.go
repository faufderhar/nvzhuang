package tryon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

// F-02：IANA 特殊用途地址必须全部禁止
func TestForbiddenAddrSpecialPurpose(t *testing.T) {
	forbidden := []string{
		"0.0.0.0", "0.1.2.3", "10.1.2.3", "100.64.0.1", "100.100.100.200", "127.0.0.1", "169.254.169.254",
		"172.16.0.1", "192.0.0.8", "192.0.2.1", "192.88.99.1", "192.168.1.1", "198.18.0.1", "198.19.255.254",
		"198.51.100.7", "203.0.113.9", "224.0.0.1", "239.255.255.250", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:100.100.100.200", "::127.0.0.1", "64:ff9b::a9fe:a9fe",
		"64:ff9b:1::1", "100::1", "2001::1", "2001:0:4136:e378:8000:63bf:3fff:fdd2", "2001:db8::1",
		"2002:a9fe:a9fe::1", "3fff::1", "5f00::1", "fc00::1", "fd12:3456::1", "fe80::1", "fec0::1", "ff02::1",
	}
	for _, raw := range forbidden {
		if !isForbiddenAddr(netip.MustParseAddr(raw)) {
			t.Errorf("expected %s to be forbidden", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "2a00:1450:4001::200e"} {
		if isForbiddenAddr(netip.MustParseAddr(raw)) {
			t.Errorf("expected public %s to be allowed", raw)
		}
	}
}

// F-02 + F-07：校验阶段拒绝 http、非 443 端口与特殊地址字面量
func TestValidateClientModelConfigHardening(t *testing.T) {
	base := ModelConfig{Provider: "openai", APIKey: "key", Model: "image-model"}
	bad := []string{
		"http://api.example.com/v1",
		"https://api.example.com:8080/v1",
		"https://api.example.com:22/v1",
		"https://100.100.100.200/latest",
		"https://198.18.0.1/v1",
		"https://[64:ff9b::a9fe:a9fe]/v1",
		"https://[2002:a9fe:a9fe::1]/v1",
		"https://240.0.0.1/v1",
		"ftp://api.example.com/v1",
		"https://user:pass@api.example.com/v1",
	}
	for _, raw := range bad {
		cfg := base
		cfg.BaseURL = raw
		if err := validateClientModelConfig(&cfg); err == nil {
			t.Errorf("expected %s to be rejected", raw)
		}
	}
	for _, raw := range []string{"https://api.example.com/v1", "https://api.example.com:443/v1", "https://8.8.8.8/v1"} {
		cfg := base
		cfg.BaseURL = raw
		if err := validateClientModelConfig(&cfg); err != nil {
			t.Errorf("expected %s to be accepted: %v", raw, err)
		}
	}
}

// F-02：拨号阶段同样拦截（防 DNS 解析到内网/fake-IP）
func TestRestrictedTransportBlocksAtDial(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()
	client := &http.Client{Transport: newRestrictedTransport(nil)}
	if _, err := client.Get(srv.URL); err == nil {
		t.Fatal("expected loopback dial to be blocked")
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatal("restricted transport reached loopback server")
	}
	// 端口白名单在拨号阶段生效
	client = &http.Client{Transport: newRestrictedTransport(map[string]bool{"443": true})}
	if _, err := client.Get("http://8.8.8.8:8080/"); err == nil || !strings.Contains(err.Error(), "端口") {
		t.Fatalf("expected port to be blocked, got %v", err)
	}
}

// F-06：跨主机重定向不得转发 x-goog-api-key
func TestGeminiRedirectDoesNotLeakKey(t *testing.T) {
	var leaked int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "" || r.Header.Get("Authorization") != "" {
			atomic.AddInt32(&leaked, 1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	gen := NewGemini(&ModelConfig{Name: "gemini", APIKey: "GEMINI-SECRET", BaseURL: origin.URL, Model: "m"})
	if _, err := gen.Generate(context.Background(), "aGVsbG8=", "aGVsbG8=", "p", ""); err == nil {
		t.Fatal("expected redirect to be refused")
	}
	if atomic.LoadInt32(&leaked) != 0 {
		t.Fatal("API key was forwarded to another host")
	}

	openai := NewOpenAIImages(&ModelConfig{Name: "custom", APIKey: "OPENAI-SECRET", BaseURL: origin.URL, Model: "m"})
	if _, err := openai.Generate(context.Background(), "aGVsbG8=", "aGVsbG8=", "p", ""); err == nil {
		t.Fatal("expected redirect to be refused")
	}
	if atomic.LoadInt32(&leaked) != 0 {
		t.Fatal("API key was forwarded to another host")
	}
}

func TestCheckModelRedirectSameHostAllowed(t *testing.T) {
	mk := func(raw string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, raw, nil)
		return r
	}
	if err := checkModelRedirect(mk("https://a.example.com/x"), []*http.Request{mk("https://a.example.com/v1")}); err != nil {
		t.Fatalf("same host redirect should be allowed: %v", err)
	}
	if err := checkModelRedirect(mk("http://a.example.com/x"), []*http.Request{mk("https://a.example.com/v1")}); err == nil {
		t.Fatal("https→http downgrade should be refused")
	}
	if err := checkModelRedirect(mk("https://b.example.com/x"), []*http.Request{mk("https://a.example.com/v1")}); err == nil {
		t.Fatal("cross host redirect should be refused")
	}
}

type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'A'
	}
	return len(p), nil
}

// F-05：上游响应体有读取上限
func TestModelResponseIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, io.LimitReader(infiniteReader{}, modelMaxResponseBytes+(1<<20)))
	}))
	defer srv.Close()
	gen := NewDoubao(&ModelConfig{Name: "doubao", APIKey: "k", BaseURL: srv.URL, Model: "m"})
	_, err := gen.Generate(context.Background(), "aGVsbG8=", "aGVsbG8=", "p", "")
	if !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("expected errResponseTooLarge, got %v", err)
	}

	big := &http.Response{StatusCode: http.StatusOK, ContentLength: modelMaxResponseBytes + 1, Body: io.NopCloser(strings.NewReader(""))}
	if _, err := readModelResponse(big); !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("expected Content-Length precheck, got %v", err)
	}
	errResp := &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(io.LimitReader(infiniteReader{}, 1<<20))}
	data, _ := readModelResponse(errResp)
	if int64(len(data)) > modelMaxErrorBodyBytes {
		t.Fatalf("error body should be capped, got %d bytes", len(data))
	}
}

// 结果图片 URL 只允许 HTTPS 公网地址
func TestDownloadResultImageRejectsUnsafeURL(t *testing.T) {
	for _, raw := range []string{"http://cdn.example.com/a.png", "https://cdn.example.com:8443/a.png", "file:///etc/passwd", "https://127.0.0.1/a.png"} {
		if _, err := downloadResultImage(context.Background(), raw); err == nil {
			t.Errorf("expected %s to be rejected", raw)
		}
	}
	r, _ := http.NewRequest(http.MethodGet, "http://cdn.example.com/a.png", nil)
	if err := checkDownloadRedirect(r, []*http.Request{r}); err == nil {
		t.Fatal("download redirect to http should be refused")
	}
}
