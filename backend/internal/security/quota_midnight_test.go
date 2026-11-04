package security

import (
	"testing"
	"time"
)

func runRefundAcrossMidnight(t *testing.T, q *DailyQuotaTracker) {
	t.Helper()
	now := time.Date(2026, 11, 3, 23, 59, 59, 0, time.UTC)
	q.SetClock(func() time.Time { return now })

	ok, day := q.Deduct("p1")
	if !ok || day != "2026-11-03" {
		t.Fatalf("Deduct = %v %q, want true 2026-11-03", ok, day)
	}
	// 新一天先产生一次成功调用
	now = now.Add(2 * time.Second)
	if ok, d2 := q.Deduct("p1"); !ok || d2 != "2026-11-04" {
		t.Fatalf("second Deduct = %v %q", ok, d2)
	}
	// 跨零点后退还第一次扣减：必须记回 11-03，不能让 11-04 的计数减少
	q.RefundDay("p1", day)
	if got := q.Used("p1"); got != 1 {
		t.Fatalf("新一天已用 = %d, want 1（退还不应记到新一天）", got)
	}
	now = now.Add(-2 * time.Second)
	if got := q.Used("p1"); got != 0 {
		t.Fatalf("前一天已用 = %d, want 0", got)
	}
}

func TestQuotaRefundAcrossMidnightMemory(t *testing.T) {
	runRefundAcrossMidnight(t, NewDailyQuotaTracker(1))
}

func TestQuotaRefundAcrossMidnightStore(t *testing.T) {
	st, err := NewBoltQuotaStore(t.TempDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	q := NewDailyQuotaTracker(1)
	q.SetStore(st)
	runRefundAcrossMidnight(t, q)
}

func TestQuotaRefundEmptyDayUsesToday(t *testing.T) {
	q := NewDailyQuotaTracker(2)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	q.SetClock(func() time.Time { return now })
	q.CheckAndDeduct("p")
	q.Refund("p")
	if q.Used("p") != 0 {
		t.Fatal("Refund 未退还当日额度")
	}
}
