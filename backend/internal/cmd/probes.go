package cmd

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"backend/internal/lifecycle"
	logictryon "backend/internal/logic/tryon"
	"backend/internal/middleware"
)

// RegisterProbes 注册存活与就绪探针。两者都不经过鉴权和限流、不调用任何付费上游，
// 生产环境应在 Nginx 中限制为仅内网 / 监控节点可访问。
//
//   - GET /healthz：进程存活即返回 200（容器 HEALTHCHECK 使用）；
//   - GET /readyz：配置已加载、至少一个云端模型已配置、状态存储可用且未进入停机流程时返回 200，否则 503。
func RegisterProbes(s *ghttp.Server, probe *lifecycle.Probe) {
	s.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.RequestID)
		group.GET("/healthz", func(r *ghttp.Request) {
			writeProbe(r, http.StatusOK, g.Map{"status": "ok"})
		})
		group.GET("/readyz", func(r *ghttp.Request) {
			ready, checks := probe.Ready(r.Context())
			status := http.StatusOK
			state := "ready"
			if !ready {
				status = http.StatusServiceUnavailable
				state = "not_ready"
			}
			writeProbe(r, status, g.Map{"status": state, "checks": checks})
		})
	})
}

func writeProbe(r *ghttp.Request, status int, body g.Map) {
	r.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
	r.Response.Header().Set("Cache-Control", "no-store")
	r.Response.WriteHeader(status)
	r.Response.WriteJson(body)
	r.ExitAll()
}

// DefaultReadinessChecks 注册与业务无关的基础就绪检查项。
func DefaultReadinessChecks(probe *lifecycle.Probe) {
	probe.AddCheck("config", func(ctx context.Context) error {
		if !g.Cfg().Available(ctx) {
			return errors.New("config unavailable")
		}
		return nil
	})
	probe.AddCheck("models", func(ctx context.Context) error {
		if len(logictryon.ConfiguredCloudModels(ctx)) == 0 {
			return errors.New("no cloud model configured")
		}
		return nil
	})
}

// LoadShutdownConfig 读取 shutdown.drainDelaySeconds 与 shutdown.timeoutSeconds。
func LoadShutdownConfig(ctx context.Context) lifecycle.ShutdownConfig {
	drain := g.Cfg().MustGet(ctx, "shutdown.drainDelaySeconds", int(lifecycle.DefaultDrainDelay/time.Second)).Int()
	timeout := g.Cfg().MustGet(ctx, "shutdown.timeoutSeconds", int(lifecycle.DefaultShutdownTimeout/time.Second)).Int()
	return lifecycle.ShutdownConfig{
		DrainDelay: time.Duration(drain) * time.Second,
		Timeout:    time.Duration(timeout) * time.Second,
	}.Normalize()
}

// Serve 启动 HTTP 服务并阻塞到 stop 被取消（通常由 SIGTERM/SIGINT 触发），随后执行优雅停机，
// 详见 WaitAndShutdown。
//
// 不使用 GoFrame 自带的信号处理：实测其收到 SIGTERM 后会立即中断在途请求（gracefulTimeout 默认仅 2 秒）。
func Serve(stop context.Context, s *ghttp.Server, probe *lifecycle.Probe, cfg lifecycle.ShutdownConfig, onStopped ...func()) error {
	if err := StartServer(s, cfg); err != nil {
		return err
	}
	return WaitAndShutdown(stop, s, probe, cfg, onStopped...)
}

// StartServer 按停机参数设置 GoFrame 的优雅关闭超时并启动监听（非阻塞）。
func StartServer(s *ghttp.Server, cfg lifecycle.ShutdownConfig) error {
	cfg = cfg.Normalize()
	secs := int((cfg.Timeout + time.Second - 1) / time.Second)
	s.SetGracefulTimeout(secs)
	s.SetGracefulShutdownTimeout(secs)
	return s.Start()
}

// WaitAndShutdown 阻塞到 stop 被取消，然后：
//  1. 标记停机，/readyz 立即返回 503；
//  2. 等待 DrainDelay（期间仍正常处理新请求，给负载均衡摘除时间）；
//  3. 关闭监听并等待在途请求完成，最长 Timeout（在途的换装请求不会被中断）；
//  4. 依次执行 onStopped（例如关闭状态存储）。
func WaitAndShutdown(stop context.Context, s *ghttp.Server, probe *lifecycle.Probe, cfg lifecycle.ShutdownConfig, onStopped ...func()) error {
	cfg = cfg.Normalize()
	<-stop.Done()

	ctx := context.Background()
	probe.SetDraining()
	g.Log().Infof(ctx, "收到停机信号：/readyz 已返回 503，%s 后停止接收新连接，最多等待在途请求 %s", cfg.DrainDelay, cfg.Timeout)
	if cfg.DrainDelay > 0 {
		time.Sleep(cfg.DrainDelay)
	}
	start := time.Now()
	err := s.Shutdown()
	g.Log().Infof(ctx, "HTTP 服务已关闭，用时 %s", time.Since(start).Round(time.Millisecond))
	for _, fn := range onStopped {
		if fn != nil {
			fn()
		}
	}
	return err
}
