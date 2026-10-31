package security

import (
	"regexp"
	"strings"
	"testing"
)

var openAIKeyPattern = regexp.MustCompile(`(sk-[A-Za-z0-9]{20,}|AIza[A-Za-z0-9]{20,})`)

// FuzzRedactSecrets 脱敏后不得残留形如 sk-xxx 的密钥或调用方声明的敏感串。
func FuzzRedactSecrets(f *testing.F) {
	f.Add("Incorrect API key provided: sk-"+strings.Repeat("A", 24)+" at 10.0.0.5", "user-secret-value")
	f.Add("nothing here", "")
	f.Fuzz(func(t *testing.T, s, sensitive string) {
		out := RedactSecrets(s, sensitive)
		if openAIKeyPattern.MatchString(out) {
			t.Fatalf("key-like string survived redaction: %q", out)
		}
		if v := strings.TrimSpace(sensitive); len(v) >= 8 && strings.Contains(out, v) {
			t.Fatalf("sensitive value survived redaction: %q", out)
		}
	})
}
