package security

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// DailyQuotaTracker 按主体统计每日（UTC 自然日）成功调用次数。
// 默认只在内存中计数（重启清零）；通过 SetStore 设置持久化后端后以存储为准，
// 存储出错时自动降级为内存计数并打印限频告警，下次存储操作成功后恢复。
type DailyQuotaTracker struct {
	mu          sync.Mutex
	dailyLimit  int
	usage       map[string]int
	lastCleanup time.Time

	store        QuotaStore
	degraded     atomic.Bool
	lastWarnUnix atomic.Int64
}

func NewDailyQuotaTracker(dailyLimit int) *DailyQuotaTracker {
	if dailyLimit <= 0 {
		dailyLimit = 100
	}
	return &DailyQuotaTracker{
		dailyLimit:  dailyLimit,
		usage:       make(map[string]int),
		lastCleanup: time.Now().UTC(),
	}
}

// SetStore 设置持久化后端（nil 表示纯内存）。应在开始处理请求前调用。
func (q *DailyQuotaTracker) SetStore(store QuotaStore) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.store = store
	q.degraded.Store(false)
}

// Store 返回当前持久化后端（可能为 nil）。
func (q *DailyQuotaTracker) Store() QuotaStore {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.store
}

// Degraded 表示最近一次存储操作失败、当前正以内存计数兜底。
func (q *DailyQuotaTracker) Degraded() bool { return q.degraded.Load() }

func (q *DailyQuotaTracker) storeFailed(err error) {
	q.degraded.Store(true)
	now := time.Now().Unix()
	last := q.lastWarnUnix.Load()
	if now-last >= 60 && q.lastWarnUnix.CompareAndSwap(last, now) {
		// 只记录错误本身（不含主体或 key）
		g.Log().Warningf(context.Background(), "【额度存储降级】持久化额度存储操作失败，暂以内存计数: %v", err)
	}
}

func (q *DailyQuotaTracker) storeOK() { q.degraded.Store(false) }

func (q *DailyQuotaTracker) CheckAndDeduct(principalID string) bool {
	if principalID == "" {
		principalID = "anonymous"
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	key := principalID + ":" + todayStr

	// 每 12 小时清理一次历史过期的日期数据
	if now.Sub(q.lastCleanup) > 12*time.Hour {
		q.cleanupLocked(now)
		if q.store != nil {
			if err := q.store.Cleanup(todayStr, now.AddDate(0, 0, -1).Format("2006-01-02")); err != nil {
				q.storeFailed(err)
			}
		}
		q.lastCleanup = now
	}

	if q.store != nil {
		ok, err := q.store.Incr(quotaStoreKey(principalID, todayStr), q.dailyLimit)
		if err == nil {
			q.storeOK()
			return ok
		}
		q.storeFailed(err)
	}

	current := q.usage[key]
	if current >= q.dailyLimit {
		return false
	}

	q.usage[key] = current + 1
	return true
}

// Refund 退还一次当日额度，用于请求在调用上游模型之前就被拒绝的情况（例如参数/配置错误）。
func (q *DailyQuotaTracker) Refund(principalID string) {
	if principalID == "" {
		principalID = "anonymous"
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	if q.store != nil {
		if err := q.store.Decr(quotaStoreKey(principalID, today)); err == nil {
			q.storeOK()
			return
		} else {
			q.storeFailed(err)
		}
	}
	key := principalID + ":" + today
	if q.usage[key] > 0 {
		q.usage[key]--
	}
}

// Used 返回当日已用额度（测试与监控用）。
func (q *DailyQuotaTracker) Used(principalID string) int {
	if principalID == "" {
		principalID = "anonymous"
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	if q.store != nil {
		if n, err := q.store.Get(quotaStoreKey(principalID, today)); err == nil {
			return n
		}
	}
	return q.usage[principalID+":"+today]
}

func (q *DailyQuotaTracker) cleanupLocked(now time.Time) {
	todayStr := now.Format("2006-01-02")
	yesterdayStr := now.AddDate(0, 0, -1).Format("2006-01-02")
	for k := range q.usage {
		// 只保留今天和昨天的条目
		if len(k) > 10 {
			datePart := k[len(k)-10:]
			if datePart != todayStr && datePart != yesterdayStr {
				delete(q.usage, k)
			}
		}
	}
}
