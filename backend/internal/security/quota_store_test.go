package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *BoltQuotaStore {
	t.Helper()
	st, err := NewBoltQuotaStore(t.TempDir(), 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestBoltQuotaStorePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := NewBoltQuotaStore(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	q := NewDailyQuotaTracker(3)
	q.SetStore(st)
	for i := 0; i < 3; i++ {
		if !q.CheckAndDeduct("ip:198.51.100.7") {
			t.Fatalf("第 %d 次应允许", i+1)
		}
	}
	if q.CheckAndDeduct("ip:198.51.100.7") {
		t.Fatal("超过额度应拒绝")
	}
	q.Refund("ip:198.51.100.7")

	// 模拟进程重启：新的 tracker + 新的 store 指向同一目录
	st2, err := NewBoltQuotaStore(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	q2 := NewDailyQuotaTracker(3)
	q2.SetStore(st2)
	if got := q2.Used("ip:198.51.100.7"); got != 2 {
		t.Fatalf("重启后应保留已用额度 2，得到 %d", got)
	}
	if !q2.CheckAndDeduct("ip:198.51.100.7") || q2.CheckAndDeduct("ip:198.51.100.7") {
		t.Fatal("重启后额度应继续累计")
	}
	if q2.Degraded() {
		t.Fatal("不应降级")
	}
}

func TestBoltQuotaStoreDoesNotPersistRawPrincipal(t *testing.T) {
	st := newTestStore(t)
	q := NewDailyQuotaTracker(5)
	q.SetStore(st)
	q.CheckAndDeduct("ip:203.0.113.99")
	raw, err := os.ReadFile(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "203.0.113.99") {
		t.Fatal("落盘数据不应包含原始 IP")
	}
}

func TestBoltQuotaStoreCorruptFileRecovered(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, quotaDBFile), []byte(strings.Repeat("not a bolt db ", 400)), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewBoltQuotaStore(dir, time.Second)
	if err != nil {
		t.Fatalf("损坏文件应被移走并重建: %v", err)
	}
	if ok, err := st.Incr("k:2026-01-01", 1); !ok || err != nil {
		t.Fatalf("重建后应可写: %v %v", ok, err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, quotaDBFile+".corrupt-*"))
	if len(matches) != 1 {
		t.Fatalf("应保留一个损坏备份文件，得到 %v", matches)
	}
}

func TestBoltQuotaStoreCleanup(t *testing.T) {
	st := newTestStore(t)
	for _, k := range []string{"a:2026-01-01", "a:2026-01-02", "b:2026-01-03"} {
		if _, err := st.Incr(k, 10); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Cleanup("2026-01-02", "2026-01-03"); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]int{"a:2026-01-01": 0, "a:2026-01-02": 1, "b:2026-01-03": 1} {
		if n, _ := st.Get(k); n != want {
			t.Fatalf("%s: want %d got %d", k, want, n)
		}
	}
}

// 两个 store 实例（模拟蓝绿切换期间的新旧容器）并发写同一文件，计数不丢失且不超过上限。
func TestBoltQuotaStoreSharedFileConcurrent(t *testing.T) {
	dir := t.TempDir()
	a, err := NewBoltQuotaStore(dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBoltQuotaStore(dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 40; i++ {
		st := a
		if i%2 == 1 {
			st = b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := st.Incr("p:2026-01-01", 30)
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if n, _ := a.Get("p:2026-01-01"); n != 30 || allowed != 30 {
		t.Fatalf("应恰好允许 30 次: count=%d allowed=%d", n, allowed)
	}
}

type failingStore struct{ fail bool }

func (f *failingStore) err() error {
	if f.fail {
		return errors.New("disk unavailable")
	}
	return nil
}
func (f *failingStore) Incr(string, int) (bool, error) { return true, f.err() }
func (f *failingStore) Decr(string) error              { return f.err() }
func (f *failingStore) Get(string) (int, error)        { return 0, f.err() }
func (f *failingStore) Cleanup(...string) error        { return f.err() }
func (f *failingStore) Ping() error                    { return f.err() }
func (f *failingStore) Close() error                   { return nil }

func TestDailyQuotaFallsBackToMemoryOnStoreError(t *testing.T) {
	fs := &failingStore{fail: true}
	q := NewDailyQuotaTracker(2)
	q.SetStore(fs)
	if !q.CheckAndDeduct("p") || !q.CheckAndDeduct("p") {
		t.Fatal("降级后仍应按内存计数放行")
	}
	if q.CheckAndDeduct("p") {
		t.Fatal("降级后内存计数仍须执行上限")
	}
	if !q.Degraded() {
		t.Fatal("应标记为降级")
	}
	fs.fail = false
	q.CheckAndDeduct("q")
	if q.Degraded() {
		t.Fatal("存储恢复后应解除降级")
	}
}

func TestNewBoltQuotaStoreRejectsEmptyDir(t *testing.T) {
	if _, err := NewBoltQuotaStore("  ", time.Second); err == nil {
		t.Fatal("空目录应报错")
	}
}
