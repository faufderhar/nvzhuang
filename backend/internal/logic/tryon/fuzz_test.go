package tryon

import (
	"net/netip"
	"net/url"
	"testing"
)

// FuzzValidateClientModelConfig 校验浏览器自定义 Base URL 的不变量：
// 通过校验的地址必须是 HTTPS、端口在允许列表内、主机不是本机或内网 IP 字面量。
func FuzzValidateClientModelConfig(f *testing.F) {
	for _, s := range []string{
		"https://api.openai.com/v1", "http://api.example.com", "https://127.0.0.1/v1", "https://[::ffff:127.0.0.1]/",
		"https://100.100.100.200/latest", "https://api.example.com:8443/v1", "https://user:pw@host/", "https://2130706433/",
		"https://[64:ff9b::a9fe:a9fe]/", "https://localhost./", "https://a.localhost/", "https://[fe80::1%25eth0]/",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		cfg := &ModelConfig{Provider: "openai", APIKey: "k", Model: "m", BaseURL: raw}
		if err := validateClientModelConfig(cfg); err != nil {
			return
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("accepted unparsable URL %q", raw)
		}
		if u.Scheme != "https" {
			t.Fatalf("accepted non-https URL %q", raw)
		}
		if !isAllowedClientPort(u.Port()) {
			t.Fatalf("accepted disallowed port %q", raw)
		}
		if addr, err := netip.ParseAddr(u.Hostname()); err == nil && isForbiddenAddr(addr) {
			t.Fatalf("accepted forbidden IP literal %q", raw)
		}
	})
}
