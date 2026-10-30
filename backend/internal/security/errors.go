package security

import "fmt"

type APIError struct {
	HTTPStatus int    `json:"-"`
	BizCode    int    `json:"code"`
	ErrorCode  string `json:"error_code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("[%d:%s] %s", e.BizCode, e.ErrorCode, e.Message)
}

func NewAPIError(httpStatus, bizCode int, errCode, message string) *APIError {
	return &APIError{
		HTTPStatus: httpStatus,
		BizCode:    bizCode,
		ErrorCode:  errCode,
		Message:    message,
	}
}

var (
	ErrAuthRequired = NewAPIError(401, 40101, "AUTH_REQUIRED", "请在设置中心填写后端访问 Token")
	ErrAuthInvalid  = NewAPIError(401, 40102, "AUTH_INVALID", "后端访问 Token 无效")

	ErrOriginNotAllowed = NewAPIError(403, 40301, "ORIGIN_NOT_ALLOWED", "当前页面来源不允许访问后端")

	ErrRateLimited        = NewAPIError(429, 42901, "RATE_LIMITED", "请求过于频繁，请稍后重试")
	ErrDailyQuotaExceeded = NewAPIError(429, 42902, "DAILY_QUOTA_EXCEEDED", "今日生成额度已用完")
	ErrConcurrencyLimited = NewAPIError(429, 42903, "CONCURRENCY_LIMITED", "当前生成任务较多，请稍后重试")

	ErrInvalidRequest = NewAPIError(400, 40001, "INVALID_REQUEST", "请求参数无效")

	ErrRequestTooLarge         = NewAPIError(413, 41301, "REQUEST_TOO_LARGE", "上传内容过大，请压缩图片后重试")
	ErrInvalidImageBase64      = NewAPIError(422, 42201, "INVALID_IMAGE_BASE64", "图片数据损坏，请重新选择图片")
	ErrUnsupportedImageType    = NewAPIError(422, 42202, "UNSUPPORTED_IMAGE_TYPE", "仅支持 JPEG、PNG 和 WebP")
	ErrImageFileTooLarge       = NewAPIError(422, 42203, "IMAGE_FILE_TOO_LARGE", "图片文件过大，请压缩后重试")
	ErrImageDimensionsInvalid  = NewAPIError(422, 42204, "IMAGE_DIMENSIONS_INVALID", "图片尺寸不符合要求")
	ErrImagePixelLimitExceeded = NewAPIError(422, 42205, "IMAGE_PIXEL_LIMIT_EXCEEDED", "图片像素过高，请缩小后重试")

	ErrInternal       = NewAPIError(500, 50001, "INTERNAL_ERROR", "服务内部错误，请稍后重试")
	ErrUpstreamFailed = NewAPIError(502, 50201, "UPSTREAM_FAILED", "换装生成失败，请稍后重试")
)
