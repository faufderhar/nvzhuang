package tryon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func TestLoadModelRequestTimeout(t *testing.T) {
	old := g.Cfg().GetAdapter()
	t.Cleanup(func() { g.Cfg().SetAdapter(old) })
	cases := []struct {
		content string
		want    time.Duration
		wantErr bool
	}{
		{"llm:\n  models: {}\n", DefaultModelRequestTimeout, false},
		{"llm:\n  requestTimeoutSeconds: 240\n", 240 * time.Second, false},
		{"llm:\n  requestTimeoutSeconds: 5\n", 0, true},
		{"llm:\n  requestTimeoutSeconds: 99999\n", 0, true},
	}
	for _, c := range cases {
		a, err := gcfg.NewAdapterContent(c.content)
		if err != nil {
			t.Fatal(err)
		}
		g.Cfg().SetAdapter(a)
		got, err := LoadModelRequestTimeout(context.Background())
		if (err != nil) != c.wantErr || got != c.want {
			t.Fatalf("%q: got %v err=%v", c.content, got, err)
		}
	}
}

func TestModelHTTPClientUsesConfiguredTimeout(t *testing.T) {
	t.Cleanup(func() { SetModelRequestTimeout(DefaultModelRequestTimeout) })
	SetModelRequestTimeout(42 * time.Second)
	if c := newModelHTTPClient(false); c.Timeout != 42*time.Second {
		t.Fatalf("服务端模型客户端超时: %v", c.Timeout)
	}
	if c := newModelHTTPClient(true); c.Timeout != 42*time.Second {
		t.Fatalf("浏览器模型客户端超时: %v", c.Timeout)
	}
	SetModelRequestTimeout(0)
	if ModelRequestTimeout() != DefaultModelRequestTimeout {
		t.Fatal("非法值应回退默认 180 秒")
	}
}

func TestCheckTimeoutBudget(t *testing.T) {
	if msg := CheckTimeoutBudget(180*time.Second, 190*time.Second); msg != "" {
		t.Fatalf("默认 180/190 应通过: %s", msg)
	}
	if msg := CheckTimeoutBudget(180*time.Second, 182*time.Second); !strings.Contains(msg, "shutdown.timeoutSeconds") {
		t.Fatalf("余量不足应告警: %q", msg)
	}
}
