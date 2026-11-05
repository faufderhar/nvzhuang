package tryon

import (
	"context"
	"strings"
)

// grokImageRef xAI 多图编辑的单张参考图。url 可以是公网地址或 data URI。
type grokImageRef struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// grokEditRequest POST {baseURL}/images/edits 的请求体。
// 官方多图编辑把参考图放在 images 数组里，顺序就是提示词中的第 1 张、第 2 张。
// 文档：https://docs.x.ai/developers/model-capabilities/images/multi-image-editing
type grokEditRequest struct {
	Model          string         `json:"model"`
	Prompt         string         `json:"prompt"`
	Images         []grokImageRef `json:"images"`
	AspectRatio    string         `json:"aspect_ratio,omitempty"`
	ResponseFormat string         `json:"response_format,omitempty"`
	// 编辑接口允许 low / medium / auto。不传时 auto 在编辑上目前等价于 medium；显式写明，避免误用文生图的 low。
	Quality string `json:"quality,omitempty"`
}

func (o *OpenAIImages) generateGrokImagine(ctx context.Context, personB64, garmentB64, prompt, size string) (string, error) {
	reqBody := grokEditRequest{
		Model:  o.model,
		Prompt: prompt,
		Images: []grokImageRef{
			{Type: "image_url", URL: inputImageDataURL(personB64)},
			{Type: "image_url", URL: inputImageDataURL(garmentB64)},
		},
		AspectRatio:    grokAspectRatio(size),
		ResponseFormat: "b64_json",
		Quality:        "medium",
	}
	return postImageGeneration(ctx, o.client, o.baseURL+"/images/edits", o.apiKey, reqBody, "图像 API", o.resultOrigin)
}

// grokAspectRatio 把前端的宽x高映射为 grok-imagine-image 接受的 aspect_ratio。
// 换装默认竖版 2:3，对应现有请求里的 1024x1536。
func grokAspectRatio(size string) string {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "1024x1024", "1:1":
		return "1:1"
	case "1536x1024", "1792x1024", "3:2":
		return "3:2"
	case "auto":
		return "auto"
	default:
		return "2:3"
	}
}
