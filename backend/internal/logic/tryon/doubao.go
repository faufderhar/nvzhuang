package tryon

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Doubao 火山方舟（豆包 Seedream）图像生成适配器。
// 注意：豆包虽复用 /images/generations 路径，但请求体是火山自定义格式：
//   - prompt + image[]（多图生图，做换装用）
//   - 返回 data[].url（不是 b64_json）
type Doubao struct {
	apiKey  string
	baseURL string
	// resultOrigin 服务端配置时为 Base URL 的 origin，结果图片与其同源时允许下载内网/非 443 地址。
	resultOrigin string
	model        string
	name         string
	client       *http.Client
}

func NewDoubao(cfg *ModelConfig) *Doubao {
	return &Doubao{
		apiKey:       cfg.APIKey,
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		resultOrigin: trustedResultOrigin(cfg),
		model:        cfg.Model,
		name:         cfg.Name,
		client:       newModelHTTPClient(cfg.ClientProvided),
	}
}

func (d *Doubao) Name() string {
	if d.name == "" {
		return "doubao"
	}
	return d.name
}

// doubaoRequest 豆包图片生成请求体（多图生图）
type doubaoRequest struct {
	Model          string   `json:"model"`
	Prompt         string   `json:"prompt"`
	Image          []string `json:"image,omitempty"`           // 参考图 URL（多图生图）
	Size           string   `json:"size,omitempty"`            // 如 "2048x2048" / "1024x1536"
	ResponseFormat string   `json:"response_format,omitempty"` // url / b64_json
	Watermark      bool     `json:"watermark,omitempty"`
}

func (d *Doubao) Generate(ctx context.Context, personB64, garmentB64, prompt, size string) (string, error) {
	if d.apiKey == "" {
		return "", fmt.Errorf("豆包 API Key 未配置（llm.models.%s.apiKey 或环境变量 ARK_API_KEY）", d.name)
	}

	// 豆包多图生图用 data URL 传参考图
	reqBody := doubaoRequest{
		Model:  d.model,
		Prompt: prompt,
		Image: []string{
			inputImageDataURL(personB64),
			inputImageDataURL(garmentB64),
		},
		Size:           normalizeDoubaoSize(size),
		ResponseFormat: "url",
	}

	return postImageGeneration(ctx, d.client, d.baseURL, d.apiKey, reqBody, "豆包 API", d.resultOrigin)
}

// normalizeDoubaoSize 把 OpenAI 风格的尺寸转成豆包支持的像素格式
// 豆包 Seedream 支持 "2048x2048"、"1024x1536"、"1536x1024" 等；OpenAI 用 "1024x1024" 等
func normalizeDoubaoSize(size string) string {
	switch size {
	case "", "auto", "1024x1024", "1024x1792", "1792x1024":
		// 换装默认用竖版半身/全身：1024x1536（2:3）
		return "1024x1536"
	case "1024x1536", "1536x1024", "2048x2048", "2048x3072", "3072x2048", "4096x4096":
		return size
	default:
		return "1024x1536"
	}
}
