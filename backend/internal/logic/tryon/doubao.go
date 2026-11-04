package tryon

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
)

// DefaultDoubaoModel 默认的豆包模型：Seedream 5.0 flash 支持多参考图生图（最多 10 张），火山方舟模型列表中为推荐、未标记下线；
// 不支持组图参数 sequential_image_generation，size 支持 1K/1.5K/2K 档位或总像素 [921600, 4624220] 的宽x高。
// doubao-seedream-3-0-t2i-* 只支持文生图，会忽略人像与服装参考图，不适合换装。
// 参考：https://ark.volcengine.com/region:cn-beijing/docs/model-list 、
// https://ark.volcengine.com/region:cn-beijing/docs/image-generation-api
const DefaultDoubaoModel = "doubao-seedream-5-0-flash-260915"

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
	warnT2I      sync.Once
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
	Watermark      bool     `json:"watermark"`                 // 官方默认 true（右下角“AI 生成”），这里显式关闭
}

func (d *Doubao) Generate(ctx context.Context, personB64, garmentB64, prompt, size string) (string, error) {
	if d.apiKey == "" {
		return "", fmt.Errorf("豆包 API Key 未配置（llm.models.%s.apiKey 或环境变量 ARK_API_KEY）", d.name)
	}

	if isDoubaoTextOnlyModel(d.model) {
		d.warnT2I.Do(func() {
			g.Log().Warningf(ctx, "【豆包模型】%s 是文生图模型，会忽略人像与服装参考图，换装效果不可用；请改用 %s 或其它支持多参考图的 Seedream 4.x/5.x 模型", SafeModelAlias(d.model), DefaultDoubaoModel)
		})
	}

	// 豆包多图生图用 data URL（data:image/<小写格式>;base64,...）传参考图，顺序为图1 人像、图2 服装
	reqBody := doubaoRequest{
		Model:  d.model,
		Prompt: prompt,
		Image: []string{
			inputImageDataURL(personB64),
			inputImageDataURL(garmentB64),
		},
		Size:           normalizeDoubaoSize(size),
		ResponseFormat: "url",
		Watermark:      false,
	}

	return postImageGeneration(ctx, d.client, d.baseURL, d.apiKey, reqBody, "豆包 API", d.resultOrigin)
}

// isDoubaoTextOnlyModel 识别只支持文生图（不接受参考图）的豆包模型，如 doubao-seedream-3-0-t2i-250415。
func isDoubaoTextOnlyModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "t2i")
}

// normalizeDoubaoSize 把 OpenAI 风格的尺寸映射为 Seedream 4.0 / 4.5 / 5.0 都接受的像素值。
// 尺寸与模型无关：官方限制（图片生成 API 文档 size 字段）为 5.0 pro/flash 总像素 [921600, 4624220]，
// 4.5 与 5.0 lite 总像素 [3686400, 16777216]，4.0 为 [921600, 16777216]；三者交集内取官方 2K 档参考值：
// 竖版 2:3 → 1664x2496，横版 3:2 → 2496x1664，方形 → 2048x2048。
func normalizeDoubaoSize(size string) string {
	switch size {
	case "1536x1024", "1792x1024", "2496x1664", "3072x2048":
		return "2496x1664"
	case "1024x1024", "2048x2048", "4096x4096":
		return "2048x2048"
	default:
		// 换装默认竖版全身（含 auto、空值与其它竖版尺寸）
		return "1664x2496"
	}
}
