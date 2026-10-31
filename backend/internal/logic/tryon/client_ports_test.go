package tryon

import (
	"strings"
	"testing"
)

func TestSetClientAllowedPorts(t *testing.T) {
	defer SetClientAllowedPorts([]int{443})

	cfg := &ModelConfig{Provider: "openai", APIKey: "k", Model: "m", BaseURL: "https://relay.example.com:8443/v1"}
	err := validateClientModelConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "443") {
		t.Fatalf("8443 should be rejected by default with a hint listing 443, got %v", err)
	}

	SetClientAllowedPorts([]int{443, 8443})
	if err := validateClientModelConfig(cfg); err != nil {
		t.Fatalf("8443 should be accepted once configured: %v", err)
	}
	if got := clientAllowedPortsText(); got != "443、8443" {
		t.Fatalf("ports text = %q", got)
	}
	if !currentClientAllowedPorts()["8443"] {
		t.Fatal("restricted transport should receive configured ports")
	}
	// 仍然必须是 HTTPS 且不能是内网
	for _, raw := range []string{"http://relay.example.com:8443/v1", "https://10.0.0.1:8443/v1"} {
		if err := validateClientModelConfig(&ModelConfig{Provider: "openai", APIKey: "k", Model: "m", BaseURL: raw}); err == nil {
			t.Errorf("%s must still be rejected", raw)
		}
	}

	SetClientAllowedPorts(nil)
	if !isAllowedClientPort("443") || isAllowedClientPort("8443") {
		t.Fatal("empty list should fall back to 443 only")
	}
	SetClientAllowedPorts([]int{0, 70000})
	if !isAllowedClientPort("443") || isAllowedClientPort("0") {
		t.Fatal("invalid ports should fall back to 443 only")
	}
}
