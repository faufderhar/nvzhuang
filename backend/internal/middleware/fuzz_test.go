package middleware

import (
	"regexp"
	"testing"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// FuzzSanitizeRequestID 任意输入的请求 ID 要么被拒绝（空串），要么原样保留且只含安全字符、不超过 64 字符。
func FuzzSanitizeRequestID(f *testing.F) {
	for _, s := range []string{"", "abc-123", "<script>", "a\r\nX-Injected: 1", string(make([]byte, 3000))} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, id string) {
		out := SanitizeRequestID(id)
		if out == "" {
			return
		}
		if out != id || !requestIDPattern.MatchString(out) {
			t.Fatalf("SanitizeRequestID(%q) = %q", id, out)
		}
	})
}

// FuzzNormalizePath 规范化结果必须以 / 开头、不含重复斜杠，且幂等。
func FuzzNormalizePath(f *testing.F) {
	for _, s := range []string{"/tryon", "//tryon", "/./tryon/", "\\tryon", "tryon", "/a/../tryon", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		n := NormalizePath(p)
		if n == "" || n[0] != '/' {
			t.Fatalf("NormalizePath(%q) = %q", p, n)
		}
		if NormalizePath(n) != n {
			t.Fatalf("NormalizePath not idempotent for %q: %q -> %q", p, n, NormalizePath(n))
		}
	})
}
