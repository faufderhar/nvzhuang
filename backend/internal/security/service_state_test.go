package security

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func initServiceWith(t *testing.T, content string) (*Service, error) {
	t.Helper()
	old := g.Cfg().GetAdapter()
	oldSvc := defaultService
	t.Cleanup(func() { g.Cfg().SetAdapter(old); defaultService = oldSvc })
	a, err := gcfg.NewAdapterContent(content)
	if err != nil {
		t.Fatal(err)
	}
	g.Cfg().SetAdapter(a)
	return InitService(context.Background())
}

func TestInitServiceStateDir(t *testing.T) {
	// 未配置：纯内存
	svc, err := initServiceWith(t, "security:\n  authRequired: false\n")
	if err != nil {
		t.Fatal(err)
	}
	if svc.DailyQuotaTracker.Store() != nil {
		t.Fatal("未配置 stateDir 时应为纯内存")
	}

	dir := filepath.Join(t.TempDir(), "state")
	svc, err = initServiceWith(t, "security:\n  authRequired: false\n  stateDir: \""+dir+"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if svc.DailyQuotaTracker.Store() == nil {
		t.Fatal("配置 stateDir 后应启用持久化")
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("状态目录应以 0700 创建: %v %v", fi, err)
	}

	// 不可用的目录（指向普通文件）：启动失败而不是静默退回内存
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	if _, err := initServiceWith(t, "security:\n  authRequired: false\n  stateDir: \""+file+"\"\n"); err == nil {
		t.Fatal("stateDir 不可用时应启动失败")
	}
}
