package tryon

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
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
		return "", "", "", gerror.NewCode(gcode.CodeInvalidParameter, "人像照片不能为空")
	}
	if garmentImg == nil || len(garmentImg.Data) == 0 {
		return "", "", "", gerror.NewCode(gcode.CodeInvalidParameter, "服装图不能为空")
	}

	personB64 := base64.StdEncoding.EncodeToString(personImg.Data)
	garmentB64 := base64.StdEncoding.EncodeToString(garmentImg.Data)

	if model == "" {
		model = "gemini"
	}

	var gen ImageGenerator
	if clientConfig != nil {
		gen, err = NewGeneratorFromClientConfig(clientConfig)
		if err != nil {
			// 个人模型配置的校验错误是固定文案（不含用户输入），返回给客户端便于用户修正。
			// 使用 NewCode 而不是 WrapCode，避免错误文案带上 "Invalid Parameter: " 前缀。
			return "", "", "", gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
		}
	} else {
		gen, err = NewGenerator(ctx, model)
		if err != nil {
			// 服务端模型配置的具体缺项（缺 Key / 缺 model 名 / provider 不支持）只写服务端日志，
			// 对外统一文案，不暴露哪些别名已配置、缺什么，也不回显未经约束的用户输入。
			reqID, _ := ctx.Value("request_id").(string)
			g.Log().Warningf(ctx, "模型配置不可用 model=%s request_id=%s err=%s",
				SafeModelAlias(model), reqID, security.RedactSecrets(err.Error()))
			return "", "", "", gerror.NewCode(gcode.CodeInvalidParameter, fmt.Sprintf("模型 %s 未配置或暂不可用", SafeModelAlias(model)))
		}
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

// modelAliasPattern 与 api/tryon/v1.TryonReq.Model 的校验规则保持一致。
var modelAliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// SafeModelAlias 返回可安全写入错误文案与日志的模型别名：不符合字符集/长度约束时返回占位符，
// 防止换行等控制字符伪造日志行（请求层已校验，这里是纵深防御）。
func SafeModelAlias(alias string) string {
	if modelAliasPattern.MatchString(alias) {
		return alias
	}
	return "[invalid]"
}
