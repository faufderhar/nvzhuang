package tryon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcfg"

	"backend/api/tryon/v1"
	"backend/internal/middleware"
	"backend/internal/security"
)

// F-13：被并发限制拒绝、或在调用上游前因参数错误失败的请求不消耗每日额度
func TestQuotaNotConsumedOnConcurrencyOrConfigError(t *testing.T) {
	secService := security.GetService()
	oldQuota, oldConc := secService.DailyQuotaTracker, secService.ConcurrencyManager
	defer func() { secService.DailyQuotaTracker, secService.ConcurrencyManager = oldQuota, oldConc }()
	secService.DailyQuotaTracker = security.NewDailyQuotaTracker(5)
	secService.ConcurrencyManager = security.NewConcurrencyManager(1, 1)

	adapter, err := gcfg.NewAdapterContent("llm:\n  models: {}\n")
	if err != nil {
		t.Fatal(err)
	}
	oldAdapter := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(oldAdapter)

	s := g.Server(uniqueServerName("test-ctrl-quota-order"))
	s.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.RequestID, middleware.HandlerResponse)
		group.Bind(NewV1())
	})
	s.SetPort(0)
	s.SetDumpRouterMap(false)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown()
	url := "http://127.0.0.1:" + strings.Split(s.GetListenedAddress(), ":")[1] + "/tryon"
	img := createTestPNG(300, 300)

	post := func(model string) int {
		body, _ := json.Marshal(v1.TryonReq{PersonImage: img, GarmentImage: img, Model: model})
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	release, ok := secService.ConcurrencyManager.Acquire("anonymous")
	if !ok {
		t.Fatal("pre-acquire failed")
	}
	if code := post("gemini"); code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 concurrency, got %d", code)
	}
	release()
	if used := secService.DailyQuotaTracker.Used("anonymous"); used != 0 {
		t.Fatalf("concurrency rejection consumed quota: %d", used)
	}

	if code := post("no-such-model-alias"); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown model, got %d", code)
	}
	if used := secService.DailyQuotaTracker.Used("anonymous"); used != 0 {
		t.Fatalf("config error should refund quota, used=%d", used)
	}
}
