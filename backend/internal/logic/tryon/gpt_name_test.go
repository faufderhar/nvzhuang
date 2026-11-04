package tryon

import "testing"

func TestIsGPTImageNameUnified(t *testing.T) {
	cases := map[string]bool{
		"gpt-image-2": true, "GPT-Image-1": true, "gpt_image_2": true, "My_GPT_IMAGE": true,
		"gemini": false, "doubao": false, "gptimage": false, "": false,
	}
	for in, want := range cases {
		if got := isGPTImageName(in); got != want {
			t.Errorf("isGPTImageName(%q)=%v, want %v", in, got, want)
		}
	}
	// 提示词选择与视觉预描述共用同一判定：gpt_image 别名两处都要识别。
	if !isGPTImageModel("custom", &ModelConfig{Name: "GPT_IMAGE"}, "custom") {
		t.Error("isGPTImageModel 应识别 GPT_IMAGE")
	}
	o := &OpenAIImages{model: "relay-gpt_image-2", name: "x"}
	if !(isGPTImageName(o.model) || isGPTImageName(o.name)) {
		t.Error("视觉预描述判定应识别 gpt_image")
	}
}
