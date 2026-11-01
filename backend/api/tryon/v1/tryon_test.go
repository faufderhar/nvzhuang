package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTryonResIncludesImageMIME(t *testing.T) {
	encoded, err := json.Marshal(TryonRes{Image: "image-data", MIME: "image/jpeg", Model: "gemini"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(encoded), `"mime":"image/jpeg"`) {
		t.Fatalf("TryonRes JSON missing mime: %s", encoded)
	}
}

// 审计 F9：响应结构不再包含系统提示词字段
func TestTryonResOmitsPrompt(t *testing.T) {
	encoded, err := json.Marshal(TryonRes{Image: "i", MIME: "image/png", Model: "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "prompt") {
		t.Fatalf("TryonRes must not expose prompt: %s", encoded)
	}
}
