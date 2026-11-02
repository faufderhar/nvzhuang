package security

import (
	"strings"
	"testing"
)

func TestRedactSecretsControlCharSplitKey(t *testing.T) {
	in := "bad key sk-00\x10" + "000000000000000000000 end"
	out := RedactSecrets(in)
	if strings.Contains(out, "sk-0") {
		t.Fatalf("control-char split key survived redaction: %q", out)
	}
}

func TestRedactSecretsGluedKey(t *testing.T) {
	out := RedactSecrets("prefix0sk-" + strings.Repeat("a", 24) + " and xAIza" + strings.Repeat("b", 24))
	if strings.Contains(out, "sk-a") || strings.Contains(out, "AIzab") {
		t.Fatalf("glued key survived redaction: %q", out)
	}
}

// 审计：上游错误体中的邮箱、手机号写日志前必须打码
func TestRedactSecretsPII(t *testing.T) {
	in := `{"error":"account alice.w+test@example-mail.co.uk quota exceeded; contact 13812345678 or +1 415-555-0132"}`
	out := RedactSecrets(in)
	for _, leak := range []string{"alice", "example-mail", "13812345678", "415-555-0132"} {
		if strings.Contains(out, leak) {
			t.Fatalf("PII %q not redacted: %s", leak, out)
		}
	}
	if !strings.Contains(out, "[EMAIL]") || !strings.Contains(out, "[PHONE]") {
		t.Fatalf("expected [EMAIL] and [PHONE] markers: %s", out)
	}
	// 普通数字（如尺寸、状态码、时间戳）不应被误判为手机号
	plain := "size 1024x1536 status 429 ts 1730000000"
	if got := RedactSecrets(plain); got != plain {
		t.Fatalf("false positive redaction: %q -> %q", plain, got)
	}
}
