package security

import (
	"reflect"
	"testing"
)

func TestClientModelAllowedHostsDefaults(t *testing.T) {
	cfg, err := loadSecurityConfigFrom(t, "security:\n  authRequired: false\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.ClientModelAllowedHosts, DefaultClientModelAllowedHosts) {
		t.Fatalf("default hosts = %v", cfg.ClientModelAllowedHosts)
	}
}

func TestClientModelAllowedHostsConfigured(t *testing.T) {
	cfg, err := loadSecurityConfigFrom(t, "security:\n  clientModelAllowedHosts: [\"API.OpenAI.com\", \"*.relay.example.net.\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"api.openai.com", "*.relay.example.net"}; !reflect.DeepEqual(cfg.ClientModelAllowedHosts, want) {
		t.Fatalf("hosts = %v, want %v", cfg.ClientModelAllowedHosts, want)
	}
}

func TestClientModelAllowedHostsRejectsInvalid(t *testing.T) {
	for _, bad := range []string{`["*"]`, `["*.com"]`, `["https://api.openai.com"]`, `["api.openai.com:443"]`, `["api.*.com"]`, `["api.openai.com/v1"]`, `["a b.com"]`} {
		if _, err := loadSecurityConfigFrom(t, "security:\n  clientModelAllowedHosts: "+bad+"\n"); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
}
