package tryon

import (
	"context"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"

	"backend/internal/security"
)

func TestClientAllowedHostsMatching(t *testing.T) {
	defer SetClientAllowedHosts(testClientAllowedHosts)
	SetClientAllowedHosts([]string{"api.openai.com", "*.relay.example.net", "  API.Corp.Example.org. ", "*", "bad host", "*.com"})

	allowed := []string{"api.openai.com", "a.relay.example.net", "x.y.relay.example.net", "api.corp.example.org"}
	for _, h := range allowed {
		if !isAllowedClientHost(h) {
			t.Errorf("%s should be allowed", h)
		}
	}
	denied := []string{"relay.example.net", "evilrelay.example.net", "api.openai.com.evil.io", "openai.com", "evil.com", "x.com"}
	for _, h := range denied {
		if isAllowedClientHost(h) {
			t.Errorf("%s should be denied", h)
		}
	}
	if n := ClientAllowedHostCount(); n != 3 {
		t.Fatalf("invalid patterns (*, 'bad host', *.com) must be ignored; got %d rules", n)
	}
}

func TestValidateClientModelConfigHostAllowlist(t *testing.T) {
	defer SetClientAllowedHosts(testClientAllowedHosts)
	SetClientAllowedHosts(security.DefaultClientModelAllowedHosts)

	base := ModelConfig{Provider: "openai", APIKey: "k", Model: "m"}
	for _, raw := range []string{
		"https://api.openai.com/v1",
		"https://API.OpenAI.com./v1",
		"https://ark.cn-beijing.volces.com/api/v3",
		"https://generativelanguage.googleapis.com/v1beta",
	} {
		cfg := base
		cfg.BaseURL = raw
		if err := validateClientModelConfig(&cfg); err != nil {
			t.Errorf("default allowlist should accept %s: %v", raw, err)
		}
	}
	for _, raw := range []string{"https://relay.attacker.example/v1", "https://8.8.8.8/v1", "https://api.openai.com.attacker.example/v1"} {
		cfg := base
		cfg.BaseURL = raw
		err := validateClientModelConfig(&cfg)
		if err == nil || !strings.Contains(err.Error(), "不在服务端允许的模型服务列表中") {
			t.Fatalf("%s must be rejected by allowlist, got %v", raw, err)
		}
		if strings.Contains(err.Error(), "attacker") || strings.Contains(err.Error(), "openai") {
			t.Fatalf("error must not echo user input or allowlist: %v", err)
		}
	}
	// 内网/本机地址的专门提示保持不变（verify.sh 依赖），HTTPS 仍为强制
	for raw, want := range map[string]string{
		"https://127.0.0.1/v1":       "内网或保留地址",
		"https://localhost/v1":       "本机地址",
		"http://api.openai.com/v1":   "HTTPS",
		"https://169.254.169.254/v1": "内网或保留地址",
	} {
		cfg := base
		cfg.BaseURL = raw
		if err := validateClientModelConfig(&cfg); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want error containing %q, got %v", raw, want, err)
		}
	}
}

func TestServerModelHosts(t *testing.T) {
	adapter, err := gcfg.NewAdapterContent(`
llm:
  models:
    a:
      provider: openai
      baseURL: "https://Relay.Example.net/v1"
    b:
      provider: openai
      baseURL: "https://relay.example.net:8443/v2"
    c:
      provider: comfyui
      baseURL: "http://127.0.0.1:8188"
    d:
      provider: openai
      baseURL: ""
`)
	if err != nil {
		t.Fatal(err)
	}
	old := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(old)

	got := ServerModelHosts(context.Background())
	if len(got) != 1 || got[0] != "relay.example.net" {
		t.Fatalf("ServerModelHosts = %v, want [relay.example.net]", got)
	}
}
