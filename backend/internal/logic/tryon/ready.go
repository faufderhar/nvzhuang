package tryon

import (
	"context"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
)

// ConfiguredCloudModels 返回服务端已完整配置（含 API Key）的云端模型别名，供 /readyz 使用。
//
// 只读取本地配置与环境变量，不发起任何网络请求。ComfyUI 需要单独的 GPU 主机，
// 不计入就绪条件（其可用性由 /comfyui/health 单独检查）。
func ConfiguredCloudModels(ctx context.Context) []string {
	val, err := g.Cfg().Get(ctx, "llm.models")
	if err != nil || val.IsEmpty() {
		return nil
	}
	var names []string
	for alias := range val.Map() {
		cfg, err := loadModelConfig(ctx, alias)
		if err != nil || cfg == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(cfg.Provider), "comfyui") {
			continue
		}
		names = append(names, alias)
	}
	sort.Strings(names)
	return names
}
