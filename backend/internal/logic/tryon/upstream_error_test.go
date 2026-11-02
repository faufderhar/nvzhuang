package tryon

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"

	"backend/internal/imagevalidate"
	"backend/internal/security"
)

// F-03：上游错误原文不得回显给客户端，服务端日志只记录脱敏内容
func TestUpstreamErrorIsSanitized(t *testing.T) {
	// 运行时拼接形似密钥的测试串，避免触发 scripts/check.sh 的敏感串扫描（这不是真实密钥）
	const leakKey = "sk-" + "proj-AB12cdEF34ghIJ56klMN78opQR90stUV"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + leakKey + `. upstream=10.0.0.5 trace=internal"}}`))
	}))
	defer upstream.Close()

	adapter, err := gcfg.NewAdapterContent(`
llm:
  models:
    leaktest:
      provider: openai
      apiKey: "server-side-key-for-test"
      baseURL: "` + upstream.URL + `"
      model: "image-model"
`)
	if err != nil {
		t.Fatal(err)
	}
	old := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(old)

	var logBuf bytes.Buffer
	logger := g.Log()
	logger.SetWriter(&logBuf)
	logger.SetStdoutPrint(false)
	defer func() {
		logger.SetWriter(nil)
		logger.SetStdoutPrint(true)
	}()

	img := &imagevalidate.ValidatedImage{Data: []byte("\x89PNG\r\n\x1a\n"), MIME: "image/png"}
	_, _, _, err = DoTryonWithValidatedImages(context.Background(), img, img, "leaktest", "", "zh", nil)
	if !errors.Is(err, security.ErrUpstreamFailed) {
		t.Fatalf("expected ErrUpstreamFailed, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-proj") || strings.Contains(err.Error(), "10.0.0.5") || strings.Contains(err.Error(), "Incorrect") {
		t.Fatalf("client-visible error leaks upstream detail: %v", err)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "换装上游调用失败") {
		t.Fatalf("expected sanitized server log, got %q", logged)
	}
	for _, secret := range []string{leakKey, "10.0.0.5", "127.0.0.1", "server-side-key-for-test"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("server log leaks %q: %s", secret, logged)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	in := "Bearer abcdefghijklmnop sk-" + strings.Repeat("abc", 9) + " AIza" + strings.Repeat("SyA12", 5) + " api_key=hunter2xyz https://relay.example.com/v1?x=1 10.0.0.5:8080 fd00::1 " + strings.Repeat("QUJD", 30) + " user-key-123456\nX: injected"
	out := security.RedactSecrets(in, "user-key-123456")
	for _, bad := range []string{"abcdefghijklmnop", "sk-abc", "AIzaSy", "hunter2", "relay.example.com", "10.0.0.5", "fd00::1", "QUJDQUJD", "user-key-123456", "\n"} {
		if strings.Contains(out, bad) {
			t.Fatalf("redaction left %q in %q", bad, out)
		}
	}
	if len(security.RedactSecrets(strings.Repeat("中", 500))) > 310 {
		t.Fatal("redacted text should be truncated")
	}
}
