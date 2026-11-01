package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

type TokenEntry struct {
	Hash        [32]byte
	PrincipalID string
}

type TokenStore struct {
	authRequired bool
	tokens       []TokenEntry
}

func NewTokenStore(authRequired bool, envKey string) (*TokenStore, error) {
	if envKey == "" {
		envKey = "TRYON_API_KEYS"
	}
	rawEnv := os.Getenv(envKey)
	var tokens []TokenEntry

	if strings.TrimSpace(rawEnv) != "" {
		parts := strings.Split(rawEnv, ",")
		for _, part := range parts {
			token := strings.TrimSpace(part)
			if token == "" {
				continue
			}
			if len(token) < 32 || len(token) > 512 {
				return nil, fmt.Errorf("环境变量 %s 中的 Token 长度无效（必须为 32 到 512 个字符）", envKey)
			}
			if isPlaceholderToken(token) {
				return nil, fmt.Errorf("环境变量 %s 中包含示例/占位 Token，请使用随机值（例如 openssl rand -hex 32）", envKey)
			}
			sum := sha256.Sum256([]byte(token))
			principalID := hex.EncodeToString(sum[:8]) // 前 16 个十六进制字符
			tokens = append(tokens, TokenEntry{
				Hash:        sum,
				PrincipalID: principalID,
			})
		}
	}

	if authRequired && len(tokens) == 0 {
		return nil, fmt.Errorf("security.authRequired 为 true，但未在环境变量 %s 中找到有效后端访问 Token（每个 Token 需 32~512 字符）", envKey)
	}

	return &TokenStore{
		authRequired: authRequired,
		tokens:       tokens,
	}, nil
}

func (s *TokenStore) IsAuthRequired() bool {
	return s.authRequired
}

func (s *TokenStore) ValidateToken(rawToken string) (string, bool) {
	token := strings.TrimSpace(rawToken)
	if len(token) < 32 || len(token) > 512 {
		return "", false
	}

	providedHash := sha256.Sum256([]byte(token))
	var matchedPrincipal string
	matched := false

	// 使用恒定时间逐个比对，不提前 break，避免泄露时序信息
	for _, entry := range s.tokens {
		if subtle.ConstantTimeCompare(providedHash[:], entry.Hash[:]) == 1 {
			matched = true
			matchedPrincipal = entry.PrincipalID
		}
	}

	if matched {
		return matchedPrincipal, true
	}
	return "", false
}

// placeholderMarkers 文档与 .env.example 中出现过的示例 Token 片段；
// 直接复制示例值上线会让任何读过文档的人都能调用接口，因此启动时拒绝。
var placeholderMarkers = []string{
	"your-", "your_", "changeme", "change-me", "change_me", "replace-me", "replace_me",
	"placeholder", "example", "with-at-least-32", "another-backend-token", "another-client-token",
	"<", ">", "openssl",
}

// isPlaceholderToken 判断 Token 是否为示例值或熵过低（不同字符过少）。
func isPlaceholderToken(token string) bool {
	lower := strings.ToLower(token)
	for _, m := range placeholderMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	distinct := make(map[rune]struct{})
	for _, r := range token {
		distinct[r] = struct{}{}
	}
	return len(distinct) < 10
}
