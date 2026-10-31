package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcfg"

	"backend/internal/lifecycle"
)

type probeBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func getProbe(t *testing.T, url string) (int, probeBody) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var b probeBody
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("探针响应应为 JSON: %q", raw)
	}
	return resp.StatusCode, b
}

// startServe 以 Serve 启动测试服务器，返回地址、停机函数与 Serve 结束通知。
func startServe(t *testing.T, name string, cfg lifecycle.ShutdownConfig, slow time.Duration, onStopped func()) (string, *lifecycle.Probe, context.CancelFunc, <-chan error) {
	t.Helper()
	probe := lifecycle.NewProbe()
	s := g.Server(uniqueServerName(name))
	s.SetPort(0)
	s.SetDumpRouterMap(false)
	RegisterProbes(s, probe)
	s.BindHandler("GET:/slow", func(r *ghttp.Request) {
		time.Sleep(slow)
		r.Response.Write("done")
	})
	stop, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	if err := StartServer(s, cfg); err != nil {
		t.Fatal(err)
	}
	port := s.GetListenedPort()
	go func() {
		done <- WaitAndShutdown(stop, s, probe, cfg, onStopped)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
		}
	})
	return "http://127.0.0.1:" + strconv.Itoa(port), probe, cancel, done
}

func TestProbesHealthzAndReadyz(t *testing.T) {
	base, probe, _, _ := startServe(t, "probes", lifecycle.ShutdownConfig{Timeout: time.Second}, 0, nil)

	if code, b := getProbe(t, base+"/healthz"); code != 200 || b.Status != "ok" {
		t.Fatalf("healthz: %d %+v", code, b)
	}
	if code, b := getProbe(t, base+"/readyz"); code != 200 || b.Status != "ready" {
		t.Fatalf("readyz 无检查项时应就绪: %d %+v", code, b)
	}
	probe.AddCheck("models", func(context.Context) error { return context.Canceled })
	if code, b := getProbe(t, base+"/readyz"); code != 503 || b.Checks["models"] != "fail" {
		t.Fatalf("模型未配置时 readyz 应 503: %d %+v", code, b)
	}
}

// O-18 验收：停机期间 readyz 先失败，在途的慢请求完整返回，之后不再接受新连接。
func TestServeGracefulShutdownDrainsInflight(t *testing.T) {
	var stopped atomic.Bool
	cfg := lifecycle.ShutdownConfig{DrainDelay: 800 * time.Millisecond, Timeout: 10 * time.Second}
	base, _, cancel, done := startServe(t, "graceful", cfg, 3*time.Second, func() { stopped.Store(true) })

	type result struct {
		code int
		body string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.Get(base + "/slow")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		resCh <- result{code: resp.StatusCode, body: string(raw)}
	}()
	time.Sleep(300 * time.Millisecond)
	cancel() // 模拟 SIGTERM

	time.Sleep(200 * time.Millisecond)
	if code, b := getProbe(t, base+"/readyz"); code != 503 || b.Checks["lifecycle"] != "draining" {
		t.Fatalf("停机等待期内 readyz 应返回 503 draining: %d %+v", code, b)
	}

	r := <-resCh
	if r.err != nil || r.code != 200 || r.body != "done" {
		t.Fatalf("在途请求应完整返回: %+v", r)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve 未在超时内返回")
	}
	if !stopped.Load() {
		t.Fatal("停机后应执行 onStopped（关闭状态存储等）")
	}
	if resp, err := http.Get(base + "/healthz"); err == nil {
		resp.Body.Close()
		t.Fatal("停机后不应再接受新连接")
	}
}

// 在途请求超过 Timeout 时不会无限等待。
func TestServeGracefulShutdownTimeout(t *testing.T) {
	cfg := lifecycle.ShutdownConfig{DrainDelay: 0, Timeout: time.Second}
	base, _, cancel, done := startServe(t, "graceful-timeout", cfg, 6*time.Second, nil)
	go func() {
		if resp, err := http.Get(base + "/slow"); err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("超过 Timeout 后 Serve 应返回")
	}
	if el := time.Since(start); el > 4*time.Second {
		t.Fatalf("停机耗时过长: %v", el)
	}
}

func TestLoadShutdownConfig(t *testing.T) {
	old := g.Cfg().GetAdapter()
	t.Cleanup(func() { g.Cfg().SetAdapter(old) })
	set := func(content string) {
		a, err := gcfg.NewAdapterContent(content)
		if err != nil {
			t.Fatal(err)
		}
		g.Cfg().SetAdapter(a)
	}

	set("server:\n  address: \":8000\"\n")
	cfg := LoadShutdownConfig(context.Background())
	if cfg.Timeout != lifecycle.DefaultShutdownTimeout || cfg.DrainDelay != lifecycle.DefaultDrainDelay {
		t.Fatalf("默认停机参数错误: %+v", cfg)
	}
	set("shutdown:\n  drainDelaySeconds: 0\n  timeoutSeconds: 250\n")
	cfg = LoadShutdownConfig(context.Background())
	if cfg.Timeout != 250*time.Second || cfg.DrainDelay != 0 {
		t.Fatalf("自定义停机参数未生效: %+v", cfg)
	}
}

// 探针与业务路由同时注册时互不影响：探针不需要 Token，/tryon 仍需鉴权。
func TestProbesCoexistWithProtectedRoutes(t *testing.T) {
	base := newRoutesTestServerWith(t, "probes-coexist", func(s *ghttp.Server) {
		RegisterProbes(s, lifecycle.NewProbe())
	})
	if code, b := getProbe(t, base+"/readyz"); code != 200 || b.Status != "ready" {
		t.Fatalf("readyz 不应需要鉴权: %d %+v", code, b)
	}
	if code, _ := doReq(t, http.MethodPost, base+"/tryon", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("/tryon 仍应需要鉴权: %d", code)
	}
}
