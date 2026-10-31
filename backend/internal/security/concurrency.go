package security

import (
	"context"
	"sync"
	"time"
)

// ConcurrencyManager 限制全局与单主体的同时生成数。
//
// 全局槽位已满时，可选地排队等待一小段时间（queueTimeout）而不是立即拒绝；
// 单主体超限始终立即拒绝（同一用户重复提交不排队）。排队中的请求同样占用单主体名额，
// 等待者总数不超过 maxWaiting，避免大量排队请求各自持有请求体内存。
type ConcurrencyManager struct {
	mu                 sync.Mutex
	globalLimit        int
	perPrincipal       int
	globalCount        int
	waiting            int
	maxWaiting         int
	perPrincipalCounts map[string]int
	// changed 在每次释放槽位时关闭，用于唤醒排队者；为 nil 表示当前没有排队者。
	changed chan struct{}
}

func NewConcurrencyManager(globalLimit, perPrincipal int) *ConcurrencyManager {
	if globalLimit <= 0 {
		globalLimit = 2
	}
	if perPrincipal <= 0 {
		perPrincipal = 1
	}
	return &ConcurrencyManager{
		globalLimit:        globalLimit,
		perPrincipal:       perPrincipal,
		maxWaiting:         globalLimit * 2,
		perPrincipalCounts: make(map[string]int),
	}
}

// Acquire 立即尝试占用槽位，不排队。
func (c *ConcurrencyManager) Acquire(principalID string) (func(), bool) {
	return c.AcquireWait(context.Background(), principalID, 0)
}

// AcquireWait 占用槽位；全局已满时最多等待 wait（<=0 表示不等待），ctx 取消（如客户端断开）时立即放弃。
func (c *ConcurrencyManager) AcquireWait(ctx context.Context, principalID string, wait time.Duration) (func(), bool) {
	if principalID == "" {
		principalID = "anonymous"
	}

	c.mu.Lock()
	if c.perPrincipalCounts[principalID] >= c.perPrincipal {
		c.mu.Unlock()
		return nil, false
	}
	// 先占用单主体名额（排队期间也计入），失败时归还
	c.perPrincipalCounts[principalID]++

	if c.globalCount < c.globalLimit {
		c.globalCount++
		c.mu.Unlock()
		return c.releaseFunc(principalID), true
	}
	if wait <= 0 || c.waiting >= c.maxWaiting {
		c.dropPrincipalLocked(principalID)
		c.mu.Unlock()
		return nil, false
	}
	c.waiting++
	c.mu.Unlock()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		c.mu.Lock()
		if c.globalCount < c.globalLimit {
			c.globalCount++
			c.waiting--
			c.mu.Unlock()
			return c.releaseFunc(principalID), true
		}
		if c.changed == nil {
			c.changed = make(chan struct{})
		}
		ch := c.changed
		c.mu.Unlock()

		select {
		case <-ch:
			continue
		case <-timer.C:
		case <-ctx.Done():
		}
		c.mu.Lock()
		// 超时与释放可能同时发生：放弃前再检查一次
		if c.globalCount < c.globalLimit {
			c.globalCount++
			c.waiting--
			c.mu.Unlock()
			return c.releaseFunc(principalID), true
		}
		c.waiting--
		c.dropPrincipalLocked(principalID)
		c.mu.Unlock()
		return nil, false
	}
}

func (c *ConcurrencyManager) releaseFunc(principalID string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.globalCount--
			if c.globalCount < 0 {
				c.globalCount = 0
			}
			c.dropPrincipalLocked(principalID)
			if c.changed != nil {
				close(c.changed)
				c.changed = nil
			}
		})
	}
}

func (c *ConcurrencyManager) dropPrincipalLocked(principalID string) {
	c.perPrincipalCounts[principalID]--
	if c.perPrincipalCounts[principalID] <= 0 {
		delete(c.perPrincipalCounts, principalID)
	}
}

// Stats 返回当前在途数与排队数（测试与监控用）。
func (c *ConcurrencyManager) Stats() (active, waiting int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.globalCount, c.waiting
}
