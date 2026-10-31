package tryon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcfg"

	"backend/api/tryon/v1"
	"backend/internal/middleware"
	"backend/internal/security"
)

// O-19：全局并发已满时按 queueTimeoutSeconds 排队；释放后继续处理，超时返回带 Retry-After 的 429。
func TestControllerConcurrencyQueue(t *testing.T) {
	secService := security.GetService()
	oldQuota, oldConc, oldCfg := secService.DailyQuotaTracker, secService.ConcurrencyManager, secService.Config
	defer func() {
		secService.DailyQuotaTracker, secService.ConcurrencyManager, secService.Config = oldQuota, oldConc, oldCfg
	}()
	cfg := *security.DefaultSecurityConfig()
	cfg.Concurrency.QueueTimeoutSeconds = 1
	secService.Config = &cfg
	secService.DailyQuotaTracker = security.NewDailyQuotaTracker(5)
	secService.ConcurrencyManager = security.NewConcurrencyManager(1, 1)

	adapter, err := gcfg.NewAdapterContent("llm:\n  models: {}\n")
	if err != nil {
		t.Fatal(err)
	}
	oldAdapter := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(oldAdapter)

	s := g.Server(uniqueServerName("test-ctrl-queue"))
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
	post := func() (*http.Response, time.Duration) {
		body, _ := json.Marshal(v1.TryonReq{PersonImage: img, GarmentImage: img, Model: "no-such-model-alias"})
		start := time.Now()
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp, time.Since(start)
	}

	// 其他主体占满全局槽位，300ms 后释放：本请求应排队后继续处理（未知模型 → 400）
	release, ok := secService.ConcurrencyManager.Acquire("other")
	if !ok {
		t.Fatal("pre-acquire")
	}
	go func() { time.Sleep(300 * time.Millisecond); release() }()
	resp, el := post()
	if resp.StatusCode != http.StatusBadRequest || el < 250*time.Millisecond {
		t.Fatalf("排队后应继续处理: status=%d elapsed=%v", resp.StatusCode, el)
	}

	// 一直不释放：排队 1 秒后返回 429 + Retry-After
	release2, _ := secService.ConcurrencyManager.Acquire("other")
	defer release2()
	resp, el = post()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "5" {
		t.Fatalf("排队超时应 429 + Retry-After: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if el < 900*time.Millisecond || el > 3*time.Second {
		t.Fatalf("应排队约 1 秒: %v", el)
	}
	if used := secService.DailyQuotaTracker.Used("anonymous"); used != 0 {
		t.Fatalf("排队超时与配置错误都不应消耗额度: %d", used)
	}
}
