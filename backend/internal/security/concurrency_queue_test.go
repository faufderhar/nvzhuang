package security

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestConcurrencyNoWaitKeepsOldBehavior(t *testing.T) {
	c := NewConcurrencyManager(1, 1)
	rel, ok := c.Acquire("a")
	if !ok {
		t.Fatal("first acquire")
	}
	start := time.Now()
	if _, ok := c.AcquireWait(context.Background(), "b", 0); ok {
		t.Fatal("wait=0 时全局已满应立即拒绝")
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("wait=0 不应阻塞")
	}
	rel()
	rel() // 重复释放无副作用
	if a, w := c.Stats(); a != 0 || w != 0 {
		t.Fatalf("stats after release: %d %d", a, w)
	}
}

func TestConcurrencyQueueAcquiresAfterRelease(t *testing.T) {
	c := NewConcurrencyManager(1, 1)
	rel, _ := c.Acquire("a")
	go func() {
		time.Sleep(150 * time.Millisecond)
		rel()
	}()
	start := time.Now()
	rel2, ok := c.AcquireWait(context.Background(), "b", 2*time.Second)
	if !ok {
		t.Fatal("释放后排队者应拿到槽位")
	}
	if el := time.Since(start); el < 100*time.Millisecond || el > time.Second {
		t.Fatalf("等待时间异常: %v", el)
	}
	rel2()
}

func TestConcurrencyQueueTimeoutAndCancel(t *testing.T) {
	c := NewConcurrencyManager(1, 1)
	rel, _ := c.Acquire("a")
	defer rel()

	start := time.Now()
	if _, ok := c.AcquireWait(context.Background(), "b", 200*time.Millisecond); ok {
		t.Fatal("超时应失败")
	}
	if el := time.Since(start); el < 150*time.Millisecond {
		t.Fatalf("应等待约 200ms: %v", el)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	start = time.Now()
	if _, ok := c.AcquireWait(ctx, "b", 5*time.Second); ok {
		t.Fatal("取消应失败")
	}
	if time.Since(start) > time.Second {
		t.Fatal("取消后应立即返回")
	}
	if a, w := c.Stats(); a != 1 || w != 0 {
		t.Fatalf("失败的排队者应归还名额: active=%d waiting=%d", a, w)
	}
	// 排队失败后同一主体仍可再次排队（单主体名额已归还）
	if _, ok := c.AcquireWait(context.Background(), "b", 10*time.Millisecond); ok {
		t.Fatal("仍应因全局已满失败")
	}
}

func TestConcurrencyPerPrincipalNeverQueues(t *testing.T) {
	c := NewConcurrencyManager(4, 1)
	rel, _ := c.Acquire("a")
	defer rel()
	start := time.Now()
	if _, ok := c.AcquireWait(context.Background(), "a", 2*time.Second); ok {
		t.Fatal("单主体超限应拒绝")
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("单主体超限应立即拒绝，不排队")
	}
}

func TestConcurrencyQueueBounded(t *testing.T) {
	c := NewConcurrencyManager(1, 1) // maxWaiting = 2
	rel, _ := c.Acquire("holder")
	var wg sync.WaitGroup
	results := make(chan bool, 3)
	for _, p := range []string{"w1", "w2"} {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			r, ok := c.AcquireWait(context.Background(), p, 2*time.Second)
			results <- ok
			if ok {
				time.Sleep(20 * time.Millisecond)
				r()
			}
		}(p)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, w := c.Stats(); w == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, ok := c.AcquireWait(context.Background(), "w3", 2*time.Second); ok {
		t.Fatal("排队已满时应立即拒绝")
	}
	rel()
	wg.Wait()
	close(results)
	for ok := range results {
		if !ok {
			t.Fatal("排队者都应依次拿到槽位")
		}
	}
	if a, w := c.Stats(); a != 0 || w != 0 {
		t.Fatalf("结束后应清零: %d %d", a, w)
	}
}

// 高并发下槽位计数不越界。
func TestConcurrencyQueueStress(t *testing.T) {
	c := NewConcurrencyManager(3, 1)
	var mu sync.Mutex
	active, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, ok := c.AcquireWait(context.Background(), string(rune('A'+i)), time.Second)
			if !ok {
				return
			}
			mu.Lock()
			active++
			if active > peak {
				peak = active
			}
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			active--
			mu.Unlock()
			r()
		}(i)
	}
	wg.Wait()
	if peak > 3 {
		t.Fatalf("同时在途数超过上限: %d", peak)
	}
	if a, w := c.Stats(); a != 0 || w != 0 {
		t.Fatalf("结束后应清零: %d %d", a, w)
	}
}
