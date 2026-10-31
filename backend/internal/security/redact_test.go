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
