package cmd

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/gfile"

	"backend/internal/controller/hello"
	"backend/internal/controller/tryon"
	"backend/internal/imagevalidate"
	"backend/internal/lifecycle"
	logictryon "backend/internal/logic/tryon"
	"backend/internal/middleware"
	"backend/internal/security"
)

// InitConfigFallback 检查并保障配置文件就绪：
// 若当前没有可用的 config.yaml，自动尝试回退加载 config.example.yaml。
func InitConfigFallback(ctx context.Context) {
	if !g.Cfg().Available(ctx) {
		candidates := []string{
			"manifest/config/config.example.yaml",
			"config/config.example.yaml",
			"config.example.yaml",
		}
		for _, path := range candidates {
			if gfile.Exists(path) {
				if adapter, ok := g.Cfg().GetAdapter().(*gcfg.AdapterFile); ok {
					adapter.SetFileName(path)
					g.Log().Infof(ctx, "未找到 config.yaml，已自动加载模板配置: %s", path)
				}
				break
			}
		}
	}
}

// EnsureServerPort 确保 Server 具备显式监听端口，避免未配置时随机分配高位端口 (:0)
func EnsureServerPort(ctx context.Context, s *ghttp.Server) {
	addr, err := g.Cfg().Get(ctx, "server.address")
	if err != nil || addr.IsEmpty() {
		s.SetPort(8000)
		g.Log().Info(ctx, "未指定 server.address 配置，已显式绑定默认端口 :8000")
	}
}

var (
	Main = gcmd.Command{
		Name:  "main",
		Usage: "main",
		Brief: "start http server",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			InitConfigFallback(ctx)

			secService, err := security.InitService(ctx)
			if err != nil {
				return err
			}

			logictryon.SetClientAllowedPorts(secService.Config.ClientModelAllowedPorts)

			modelTimeout, err := logictryon.LoadModelRequestTimeout(ctx)
			if err != nil {
				return err
			}
			logictryon.SetModelRequestTimeout(modelTimeout)
			shutdownCfg := LoadShutdownConfig(ctx)
			if msg := logictryon.CheckTimeoutBudget(modelTimeout, shutdownCfg.Timeout); msg != "" {
				g.Log().Warning(ctx, "【配置警告】"+msg)
			}
			g.Log().Infof(ctx, "模型请求超时 %s；反向代理的读超时（如 Nginx proxy_read_timeout）必须大于该值，建议 ≥ %s",
				modelTimeout, modelTimeout+2*time.Minute)

			imgLimits := imagevalidate.LoadImageLimits(ctx)

			probe := lifecycle.NewProbe()
			DefaultReadinessChecks(probe)
			AddQuotaStateCheck(probe, secService.DailyQuotaTracker)

			s := g.Server()
			EnsureServerPort(ctx, s)
			RegisterProbes(s, probe)
			RegisterRoutes(s, secService, imgLimits.MaxRequestBodyBytes)

			stop, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return Serve(stop, s, probe, shutdownCfg, func() {
				if store := secService.DailyQuotaTracker.Store(); store != nil {
					_ = store.Close()
				}
			})
		},
	}
)

// RegisterRoutes 注册全部路由与中间件。
//
// 鉴权、限流和请求体上限直接挂在业务路由组上（而不是依赖 URL 精确匹配），
// 因此 //tryon、///tryon、/tryon/ 等任何能命中处理器的写法都会经过安全中间件。
func RegisterRoutes(s *ghttp.Server, secService *security.Service, maxBodyBytes int64) {
	corsMw := middleware.NewCORSMiddleware(secService.Config)
	bodyLimitMw := middleware.NewBodyLimitMiddleware(maxBodyBytes)
	authMw := middleware.NewAuthMiddlewareWithProxies(secService.TokenStore, secService.TrustedProxies)
	rateLimitMw := middleware.NewRateLimitMiddleware(secService.RateLimiter)

	// GoFrame 默认 ClientMaxBodySize 为 8MB，会在业务中间件之前以 500 纯文本拒绝请求；
	// 这里设为业务上限 + 余量，保证超限请求统一返回 413 REQUEST_TOO_LARGE。
	s.SetClientMaxBodySize(bodyLimitMw.MaxBytes() + middleware.BodyLimitMargin)

	s.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(
			middleware.RequestID,
			middleware.HandlerResponse,
			corsMw.Handler,
		)
		group.Bind(hello.NewV1())

		// 受保护的业务接口：/tryon、/comfyui/health
		group.Group("/", func(protected *ghttp.RouterGroup) {
			protected.Middleware(
				bodyLimitMw.Enforce,
				authMw.Enforce,
				rateLimitMw.Enforce,
			)
			protected.Bind(tryon.NewV1())
		})
	})
}
