package tryon

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	// DefaultModelRequestTimeout 调用云端模型（生成图片）的 HTTP 总超时。
	DefaultModelRequestTimeout = 180 * time.Second
	minModelRequestTimeout     = 10 * time.Second
	maxModelRequestTimeout     = 30 * time.Minute
)

var modelRequestTimeout atomic.Int64

func init() {
	modelRequestTimeout.Store(int64(DefaultModelRequestTimeout))
}

// SetModelRequestTimeout 设置云端模型 HTTP 超时（启动时由 llm.requestTimeoutSeconds 设置）。
func SetModelRequestTimeout(d time.Duration) {
	if d <= 0 {
		d = DefaultModelRequestTimeout
	}
	modelRequestTimeout.Store(int64(d))
}

// ModelRequestTimeout 返回当前云端模型 HTTP 超时。
func ModelRequestTimeout() time.Duration {
	return time.Duration(modelRequestTimeout.Load())
}

// LoadModelRequestTimeout 读取 llm.requestTimeoutSeconds（默认 180 秒，允许 10–1800 秒）。
// 超出范围时返回错误，使服务启动失败而不是静默使用意外的值。
func LoadModelRequestTimeout(ctx context.Context) (time.Duration, error) {
	secs := g.Cfg().MustGet(ctx, "llm.requestTimeoutSeconds", int(DefaultModelRequestTimeout/time.Second)).Int()
	d := time.Duration(secs) * time.Second
	if d < minModelRequestTimeout || d > maxModelRequestTimeout {
		return 0, fmt.Errorf("llm.requestTimeoutSeconds 必须在 %d–%d 秒之间: %d",
			int(minModelRequestTimeout/time.Second), int(maxModelRequestTimeout/time.Second), secs)
	}
	return d, nil
}

// ShutdownMargin 优雅停机等待时间至少要比模型超时多出的余量。
const ShutdownMargin = 5 * time.Second

// CheckTimeoutBudget 校验超时链路：停机等待时间必须大于模型超时 + 余量，
// 否则重启时在途请求会被截断。返回非空字符串表示需要告警的原因。
func CheckTimeoutBudget(model, shutdown time.Duration) string {
	if shutdown < model+ShutdownMargin {
		return fmt.Sprintf("shutdown.timeoutSeconds（%s）应至少比 llm.requestTimeoutSeconds（%s）大 %s，否则重启时在途请求会被中断",
			shutdown, model, ShutdownMargin)
	}
	return ""
}
