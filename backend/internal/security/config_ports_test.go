package security

import (
	"context"
	"reflect"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func loadSecurityConfigFrom(t *testing.T, content string) (*SecurityConfig, error) {
	t.Helper()
	adapter, err := gcfg.NewAdapterContent(content)
	if err != nil {
		t.Fatal(err)
	}
	old := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(old)
	return LoadSecurityConfig(context.Background())
}

func TestClientModelAllowedPortsDefaultsTo443(t *testing.T) {
	cfg, err := loadSecurityConfigFrom(t, "security:\n  authRequired: false\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.ClientModelAllowedPorts, []int{443}) {
		t.Fatalf("default ports = %v, want [443]", cfg.ClientModelAllowedPorts)
	}
}

func TestClientModelAllowedPortsConfigured(t *testing.T) {
	cfg, err := loadSecurityConfigFrom(t, "security:\n  clientModelAllowedPorts: [443, 8443]\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.ClientModelAllowedPorts, []int{443, 8443}) {
		t.Fatalf("ports = %v, want [443 8443]", cfg.ClientModelAllowedPorts)
	}
}

func TestClientModelAllowedPortsRejectsInvalid(t *testing.T) {
	for _, bad := range []string{"[0]", "[70000]", "[-1]"} {
		if _, err := loadSecurityConfigFrom(t, "security:\n  clientModelAllowedPorts: "+bad+"\n"); err == nil {
			t.Errorf("expected %s to fail startup", bad)
		}
	}
}

func TestLoadSecurityConfigQueueTimeout(t *testing.T) {
	ctx := context.Background()
	old := g.Cfg().GetAdapter()
	t.Cleanup(func() { g.Cfg().SetAdapter(old) })
	for content, wantErr := range map[string]bool{
		"security:\n  concurrency:\n    queueTimeoutSeconds: 10\n": false,
		"security:\n  concurrency:\n    queueTimeoutSeconds: -1\n": true,
		"security:\n  concurrency:\n    queueTimeoutSeconds: 61\n": true,
	} {
		a, err := gcfg.NewAdapterContent(content)
		if err != nil {
			t.Fatal(err)
		}
		g.Cfg().SetAdapter(a)
		cfg, err := LoadSecurityConfig(ctx)
		if (err != nil) != wantErr {
			t.Fatalf("%q: err=%v", content, err)
		}
		if !wantErr && cfg.Concurrency.QueueTimeoutSeconds != 10 {
			t.Fatalf("queueTimeoutSeconds 未生效: %+v", cfg.Concurrency)
		}
	}
	if DefaultSecurityConfig().Concurrency.QueueTimeoutSeconds != 0 {
		t.Fatal("默认不排队，保持旧行为")
	}
}
