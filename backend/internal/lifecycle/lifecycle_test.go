package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProbeReadyAndDraining(t *testing.T) {
	p := NewProbe()
	ctx := context.Background()
	if ok, res := p.Ready(ctx); !ok || res["lifecycle"] != "ok" {
		t.Fatalf("无检查项时应就绪: %v %v", ok, res)
	}
	p.AddCheck("models", func(context.Context) error { return nil })
	p.AddCheck("state", func(context.Context) error { return errors.New("disk full /secret/path") })
	ok, res := p.Ready(ctx)
	if ok || res["state"] != "fail" || res["models"] != "ok" {
		t.Fatalf("state 失败时不应就绪: %v %v", ok, res)
	}
	for _, v := range res {
		if v != "ok" && v != "fail" && v != "draining" {
			t.Fatalf("结果不得包含错误细节: %q", v)
		}
	}
	p.AddCheck("state", func(context.Context) error { return nil })
	if ok, _ := p.Ready(ctx); !ok {
		t.Fatal("替换检查项后应就绪")
	}
	p.SetDraining()
	if ok, res := p.Ready(ctx); ok || res["lifecycle"] != "draining" || !p.Draining() {
		t.Fatalf("停机中应不就绪: %v %v", ok, res)
	}
}

func TestProbeSlowCheckTimesOut(t *testing.T) {
	p := NewProbe()
	p.AddCheck("slow", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	start := time.Now()
	ok, _ := p.Ready(context.Background())
	if ok || time.Since(start) > checkTimeout+time.Second {
		t.Fatalf("慢检查应超时失败: ok=%v elapsed=%v", ok, time.Since(start))
	}
}

func TestShutdownConfigNormalize(t *testing.T) {
	c := ShutdownConfig{DrainDelay: -1, Timeout: 0}.Normalize()
	if c.DrainDelay != 0 || c.Timeout != DefaultShutdownTimeout {
		t.Fatalf("默认值错误: %+v", c)
	}
	c = ShutdownConfig{DrainDelay: time.Hour, Timeout: time.Hour}.Normalize()
	if c.DrainDelay != time.Minute || c.Timeout != maxShutdownTimeout {
		t.Fatalf("上限错误: %+v", c)
	}
}
