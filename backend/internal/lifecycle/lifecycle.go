// Package lifecycle 提供存活/就绪探针与优雅停机所需的进程级状态。
//
// 探针只检查本进程内部状态（配置、模型是否已配置、状态存储是否可写），
// 绝不调用任何付费上游接口。
package lifecycle

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// CheckFunc 就绪检查项：返回 nil 表示正常。实现必须快速返回且不得调用付费上游。
type CheckFunc func(ctx context.Context) error

// Probe 维护就绪检查项与“正在停机”标记，所有方法并发安全。
type Probe struct {
	draining atomic.Bool
	mu       sync.RWMutex
	checks   map[string]CheckFunc
}

// NewProbe 创建探针。
func NewProbe() *Probe {
	return &Probe{checks: make(map[string]CheckFunc)}
}

// AddCheck 注册（或替换）一个就绪检查项。
func (p *Probe) AddCheck(name string, fn CheckFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.checks[name] = fn
}

// SetDraining 标记进程进入停机流程：此后 /readyz 返回 503，负载均衡应停止转发新请求。
func (p *Probe) SetDraining() { p.draining.Store(true) }

// Draining 是否已进入停机流程。
func (p *Probe) Draining() bool { return p.draining.Load() }

// checkTimeout 单个检查项的最长耗时，避免探针被慢检查拖住。
const checkTimeout = 2 * time.Second

// Ready 依次执行检查项，返回是否就绪以及每项的结果（只含 ok/fail/draining，不含错误细节）。
func (p *Probe) Ready(ctx context.Context) (bool, map[string]string) {
	p.mu.RLock()
	names := make([]string, 0, len(p.checks))
	checks := make(map[string]CheckFunc, len(p.checks))
	for n, fn := range p.checks {
		names = append(names, n)
		checks[n] = fn
	}
	p.mu.RUnlock()
	sort.Strings(names)

	result := make(map[string]string, len(names)+1)
	ready := true
	if p.Draining() {
		result["lifecycle"] = "draining"
		ready = false
	} else {
		result["lifecycle"] = "ok"
	}
	for _, n := range names {
		cctx, cancel := context.WithTimeout(ctx, checkTimeout)
		err := checks[n](cctx)
		cancel()
		if err != nil {
			result[n] = "fail"
			ready = false
			continue
		}
		result[n] = "ok"
	}
	return ready, result
}

// ShutdownConfig 优雅停机参数。
type ShutdownConfig struct {
	// DrainDelay 收到停机信号后、关闭监听前的等待时间：期间 /readyz 已返回 503，
	// 但仍正常处理新请求，给上游负载均衡留出摘除时间。
	DrainDelay time.Duration
	// Timeout 关闭监听后等待在途请求完成的最长时间，应大于模型请求超时。
	Timeout time.Duration
}

const (
	DefaultDrainDelay      = 3 * time.Second
	DefaultShutdownTimeout = 190 * time.Second
	maxShutdownTimeout     = 30 * time.Minute
)

// Normalize 填充默认值并限制范围。
func (c ShutdownConfig) Normalize() ShutdownConfig {
	if c.DrainDelay < 0 {
		c.DrainDelay = 0
	}
	if c.DrainDelay > time.Minute {
		c.DrainDelay = time.Minute
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultShutdownTimeout
	}
	if c.Timeout > maxShutdownTimeout {
		c.Timeout = maxShutdownTimeout
	}
	return c
}
