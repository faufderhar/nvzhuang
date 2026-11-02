package tryon

import (
	"os"
	"testing"

	"backend/internal/security"
)

// testClientAllowedHosts 本包测试使用的个人模型域名白名单：默认官方域名 + 测试用的 example.com 系列与一个公网 IP。
// 专门测试白名单行为的用例会自行调用 SetClientAllowedHosts 并在结束时恢复。
var testClientAllowedHosts = append(append([]string(nil), security.DefaultClientModelAllowedHosts...),
	"example.com", "*.example.com", "8.8.8.8")

func TestMain(m *testing.M) {
	SetClientAllowedHosts(testClientAllowedHosts)
	os.Exit(m.Run())
}
