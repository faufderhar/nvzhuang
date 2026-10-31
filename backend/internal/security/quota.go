package security

import (
	"sync"
	"time"
)

type DailyQuotaTracker struct {
	mu          sync.Mutex
	dailyLimit  int
	usage       map[string]int
	lastCleanup time.Time
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
		q.lastCleanup = now
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
	key := principalID + ":" + time.Now().UTC().Format("2006-01-02")
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
	return q.usage[principalID+":"+time.Now().UTC().Format("2006-01-02")]
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
