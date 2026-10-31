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
