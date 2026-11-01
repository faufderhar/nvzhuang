package tryon

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// DetectGeneratedImageMIME determines the real media type from the generated
// file signature instead of trusting provider headers or declarations.
func DetectGeneratedImageMIME(value string) (string, error) {
	raw := strings.TrimSpace(ensureBase64(value))
	if raw == "" {
		return "", fmt.Errorf("生成结果图片为空")
	}

	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("生成结果图片 base64 格式错误")
	}

	if mime := sniffImageMIME(data); mime != "" {
		return mime, nil
	}
	return "", fmt.Errorf("生成结果不是受支持的图片格式")
}

// sniffImageMIME 按文件签名识别 PNG / JPEG / WebP，无法识别时返回空串。
func sniffImageMIME(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// inputImageMIME 根据 base64 前若干字节的文件签名识别输入图片 MIME，用于向上游声明正确的类型；
// 无法识别时回退为 image/png（输入图片已在 imagevalidate 中完成格式校验）。
func inputImageMIME(value string) string {
	raw := strings.TrimSpace(ensureBase64(value))
	if len(raw) > 24 {
		raw = raw[:24]
	}
	data, _ := base64.StdEncoding.DecodeString(raw) // 截断后可能报错，但已解码的前缀仍可用于识别
	if mime := sniffImageMIME(data); mime != "" {
		return mime
	}
	return "image/png"
}

// inputImageDataURL 生成带正确 MIME 的 data URL。
func inputImageDataURL(value string) string {
	return "data:" + inputImageMIME(value) + ";base64," + ensureBase64(value)
}
