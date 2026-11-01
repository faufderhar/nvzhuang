package imagevalidate

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
)

type ImageLimits struct {
	MaxRequestBodyBytes     int64    `json:"maxRequestBodyBytes"`
	MaxEncodedImageBytes    int64    `json:"maxEncodedImageBytes"`
	MaxDecodedImageBytes    int64    `json:"maxDecodedImageBytes"`
	MaxCombinedDecodedBytes int64    `json:"maxCombinedDecodedBytes"`
	MinWidth                int      `json:"minWidth"`
	MinHeight               int      `json:"minHeight"`
	MaxWidth                int      `json:"maxWidth"`
	MaxHeight               int      `json:"maxHeight"`
	MaxPixels               int64    `json:"maxPixels"`
	AllowedMimeTypes        []string `json:"allowedMimeTypes"`
}

func DefaultImageLimits() ImageLimits {
	return ImageLimits{
		MaxRequestBodyBytes:     25165824, // 24 MiB
		MaxEncodedImageBytes:    12582912, // 12 MiB
		MaxDecodedImageBytes:    8388608,  // 8 MiB
		MaxCombinedDecodedBytes: 16777216, // 16 MiB
		MinWidth:                256,
		MinHeight:               256,
		MaxWidth:                8192,
		MaxHeight:               8192,
		MaxPixels:               20000000, // 2000万像素
		AllowedMimeTypes: []string{
			"image/jpeg",
			"image/png",
			"image/webp",
		},
	}
}

func LoadImageLimits(ctx context.Context) ImageLimits {
	limits := DefaultImageLimits()
	val, err := g.Cfg().Get(ctx, "upload")
	if err != nil || val.IsEmpty() {
		return limits
	}
	_ = val.Scan(&limits)

	// 非正数 / 空列表回退到默认值
	def := DefaultImageLimits()
	positiveOr(&limits.MaxRequestBodyBytes, def.MaxRequestBodyBytes)
	positiveOr(&limits.MaxEncodedImageBytes, def.MaxEncodedImageBytes)
	positiveOr(&limits.MaxDecodedImageBytes, def.MaxDecodedImageBytes)
	positiveOr(&limits.MaxCombinedDecodedBytes, def.MaxCombinedDecodedBytes)
	positiveOr(&limits.MinWidth, def.MinWidth)
	positiveOr(&limits.MinHeight, def.MinHeight)
	positiveOr(&limits.MaxWidth, def.MaxWidth)
	positiveOr(&limits.MaxHeight, def.MaxHeight)
	positiveOr(&limits.MaxPixels, def.MaxPixels)
	if len(limits.AllowedMimeTypes) == 0 {
		limits.AllowedMimeTypes = def.AllowedMimeTypes
	}
	return limits
}

func positiveOr[T int | int64](v *T, def T) {
	if *v <= 0 {
		*v = def
	}
}
