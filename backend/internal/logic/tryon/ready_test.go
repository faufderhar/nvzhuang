package tryon

import (
	"context"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func TestConfiguredCloudModels(t *testing.T) {
	ctx := context.Background()
	old := g.Cfg().GetAdapter()
	t.Cleanup(func() { g.Cfg().SetAdapter(old) })

	t.Setenv("READY_TEST_GEMINI_KEY", "")
	content := `
llm:
  models:
    gemini:
      provider: gemini
      envKey: READY_TEST_GEMINI_KEY
      baseURL: "https://generativelanguage.googleapis.com/v1beta"
      model: "m"
    comfyui:
      provider: comfyui
      baseURL: "http://127.0.0.1:8188"
      model: "q"
`
	adapter, err := gcfg.NewAdapterContent(content)
	if err != nil {
		t.Fatal(err)
	}
	g.Cfg().SetAdapter(adapter)

	if got := ConfiguredCloudModels(ctx); len(got) != 0 {
		t.Fatalf("没有 Key 且只有 ComfyUI 时不应计为就绪: %v", got)
	}
	t.Setenv("READY_TEST_GEMINI_KEY", "placeholder-value-for-test")
	got := ConfiguredCloudModels(ctx)
	if len(got) != 1 || got[0] != "gemini" {
		t.Fatalf("配置 Key 后应只返回 gemini: %v", got)
	}
}
