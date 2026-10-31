package security

import (
	"regexp"
	"strings"
)

var redactPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// 常见 API Key / Token 形态
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`), "Bearer [REDACTED]"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_*.-]{6,}`), "[REDACTED_KEY]"},
	// 紧贴在其它字母数字之后的长密钥（如 "xsk-..."）没有单词边界，单独兜底
	{regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`), "[REDACTED_KEY]"},
	{regexp.MustCompile(`AIza[A-Za-z0-9_-]{20,}`), "[REDACTED_KEY]"},
	{regexp.MustCompile(`\bAIza[A-Za-z0-9_*-]{10,}`), "[REDACTED_KEY]"},
	{regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?key|secret|token|password|authorization|x-goog-api-key)(["'\s:=]+)[^\s"',;&]+`), "$1$2[REDACTED]"},
	// URL（可能包含用户自定义 Base URL 或带签名的结果地址）
	{regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>]+`), "[URL]"},
	// IPv4 / IPv6 地址
	{regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`), "[IP]"},
	{regexp.MustCompile(`(?i)\[?[0-9a-f]{0,4}:[0-9a-f]{0,4}:[0-9a-f:.]*[0-9a-f]\]?`), "[IP]"},
	// 长串 base64 / hex（可能是图片数据或密钥）
	{regexp.MustCompile(`[A-Za-z0-9+/_=-]{40,}`), "[REDACTED_BLOB]"},
}

// RedactSecrets 对准备写入服务端日志的文本做脱敏：移除密钥、URL、IP、长串 base64，
// 并额外替换调用方明确传入的敏感值（如本次请求的 API Key），最后截断到 300 字符。
// 返回值只用于日志，绝不回显给客户端。
func RedactSecrets(s string, sensitive ...string) string {
	// 先清理控制字符再匹配，避免 "sk-12\x0034..." 这类被控制字符打断的密钥
	// 在正则匹配后才被拼接成完整密钥写入日志。
	s = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	for _, v := range sensitive {
		v = strings.TrimSpace(v)
		if len(v) >= 4 {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	for _, p := range redactPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	const maxLen = 300
	if len(s) > maxLen {
		cut := maxLen
		for cut > 0 && !isRuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "..."
	}
	return s
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
