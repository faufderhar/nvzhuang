package tryon

import (
	"context"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"

	"backend/internal/imagevalidate"
)

// 模型配置错误直接返回给用户，不能带 GoFrame 的 "Invalid Parameter: " 前缀，且错误码保持 CodeInvalidParameter（用于退还额度）
func TestModelConfigErrorHasNoCodePrefix(t *testing.T) {
	img := &imagevalidate.ValidatedImage{Data: []byte{1, 2, 3}}
	bad := &ModelConfig{Provider: "openai", APIKey: "k", Model: "m", BaseURL: "http://api.example.com/v1"}
	_, _, _, err := DoTryonWithValidatedImages(context.Background(), img, img, "", "", "zh", bad)
	if err == nil {
		t.Fatal("expected config error")
	}
	if strings.Contains(err.Error(), "Invalid Parameter") {
		t.Fatalf("error message must not carry code prefix: %q", err.Error())
	}
	if gerror.Code(err) != gcode.CodeInvalidParameter {
		t.Fatalf("error code = %v, want CodeInvalidParameter", gerror.Code(err))
	}
}
