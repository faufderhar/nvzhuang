package tryon

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	"backend/internal/imagevalidate"
	"backend/internal/security"
)

// DoTryonWithValidatedImages 使用已完成安全校验的图片结构执行换装
func DoTryonWithValidatedImages(ctx context.Context, personImg, garmentImg *imagevalidate.ValidatedImage, model, size, lang string, clientConfig *ModelConfig) (resultB64, usedModel, usedPrompt string, err error) {
	if personImg == nil || len(personImg.Data) == 0 {
		return "", "", "", gerror.New("人像照片不能为空")
	}
	if garmentImg == nil || len(garmentImg.Data) == 0 {
		return "", "", "", gerror.New("服装图不能为空")
	}

	personB64 := base64.StdEncoding.EncodeToString(personImg.Data)
	garmentB64 := base64.StdEncoding.EncodeToString(garmentImg.Data)

	if model == "" {
		model = "gemini"
	}

	var gen ImageGenerator
	if clientConfig != nil {
		gen, err = NewGeneratorFromClientConfig(clientConfig)
	} else {
		gen, err = NewGenerator(ctx, model)
	}
	if err != nil {
		// 模型选择/配置校验错误均为服务端固定文案，可返回给客户端
		return "", "", "", gerror.WrapCode(gcode.CodeInvalidParameter, err, "")
	}

	var promptStr string
	isGemini := (clientConfig != nil && strings.EqualFold(clientConfig.Provider, "gemini")) || gen.Name() == "gemini"
	if isGPTImageModel(model, clientConfig, gen.Name()) {
		promptStr = BuildGPTSafety(lang)
	} else if isGemini {
		promptStr = BuildGemini(lang)
	} else {
		promptStr = BuildDefault(lang)
	}

	result, err := gen.Generate(ctx, personB64, garmentB64, promptStr, size)
	if err != nil {
		logUpstreamError(ctx, gen.Name(), err, clientConfig)
		// 对外只返回固定文案与 request_id，原始错误（可能含上游响应、内网地址或密钥）仅以脱敏形式写入服务端日志。
		return "", "", "", security.ErrUpstreamFailed
	}

	return result, gen.Name(), promptStr, nil
}

// isGPTImageModel 判断当前请求是否使用 GPT-Image 系列模型
func isGPTImageModel(model string, clientConfig *ModelConfig, genName string) bool {
	check := func(s string) bool {
		lower := strings.ToLower(s)
		return strings.Contains(lower, "gpt-image") || strings.Contains(lower, "gpt_image")
	}
	if check(genName) || check(model) {
		return true
	}
	if clientConfig != nil && (check(clientConfig.Model) || check(clientConfig.Name)) {
		return true
	}
	return false
}

// logUpstreamError 以脱敏形式记录上游失败原因：不记录用户 Key、Base URL、IP 或图片数据。
func logUpstreamError(ctx context.Context, genName string, err error, clientConfig *ModelConfig) {
	var sensitive []string
	if clientConfig != nil {
		sensitive = append(sensitive, clientConfig.APIKey, clientConfig.BaseURL)
	}
	reqID, _ := ctx.Value("request_id").(string)
	g.Log().Warningf(ctx, "换装上游调用失败 model=%s request_id=%s err=%s",
		genName, reqID, security.RedactSecrets(err.Error(), sensitive...))
}
