package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"

	logictryon "backend/internal/logic/tryon"
	"backend/internal/security"
)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", rel))
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", rel, err)
	}
	return string(b)
}

// 上线风险回归：生产叠加配置、Nginx 模板与后端安全配置必须彼此一致。
// 1) trustedProxies 必须恰好信任 Nginx 容器的 /32（而不是 127.0.0.1 或 Docker 网桥网关）；
// 2) Nginx proxy_read_timeout 必须大于模型请求超时 + 余量，停机超时也要覆盖模型超时。
func TestProdOverridesConsistency(t *testing.T) {
	compose := readRepoFile(t, "deploy/docker-compose.prod.yml")
	tmpl := readRepoFile(t, "deploy/nginx/templates/nvzhuang.conf.template")
	overrides := readRepoFile(t, "deploy/backend/config.prod.overrides.yaml")

	addrs := regexp.MustCompile(`(?m)^  (\S+):\n(?:(?:    .*|)\n)*?        ipv4_address: (\S+)`).FindAllStringSubmatch(compose, -1)
	ips := map[string]string{}
	for _, m := range addrs {
		ips[m[1]] = m[2]
	}
	nginxIP, backendIP := ips["nginx"], ips["costume-tryon"]
	if nginxIP == "" || backendIP == "" {
		t.Fatalf("compose 中未找到固定地址: %v", ips)
	}
	if !regexp.MustCompile(`(?m)^    ports: !reset \[\]$`).MatchString(compose) {
		t.Fatal("生产叠加配置必须清除后端端口发布（ports: !reset []）")
	}
	if !regexp.MustCompile(`server ` + regexp.QuoteMeta(backendIP) + `:8000`).MatchString(tmpl) {
		t.Fatalf("Nginx upstream 必须指向后端固定地址 %s:8000", backendIP)
	}
	for _, must := range []string{
		"proxy_set_header X-Forwarded-For $remote_addr;",
		"proxy_request_buffering on;",
		"deny all;",
		"ssl_protocols TLSv1.2 TLSv1.3;",
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(must)).MatchString(tmpl) {
			t.Fatalf("Nginx 模板缺少 %q", must)
		}
	}

	adapter, err := gcfg.NewAdapterContent(overrides)
	if err != nil {
		t.Fatal(err)
	}
	old := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	t.Cleanup(func() { g.Cfg().SetAdapter(old) })
	ctx := context.Background()

	cfg, err := security.LoadSecurityConfig(ctx)
	if err != nil {
		t.Fatalf("生产覆盖配置无效: %v", err)
	}
	if !cfg.AuthRequired {
		t.Fatal("生产必须开启 authRequired")
	}
	if len(cfg.TrustedProxies) != 1 || cfg.TrustedProxies[0] != nginxIP+"/32" {
		t.Fatalf("trustedProxies 必须恰好为 [%s/32]，实际 %v", nginxIP, cfg.TrustedProxies)
	}
	if cfg.StateDir == "" {
		t.Fatal("生产应启用额度持久化 stateDir")
	}
	if !regexp.MustCompile(`tryon-state:` + regexp.QuoteMeta(cfg.StateDir)).MatchString(compose) {
		t.Fatalf("compose 必须把 tryon-state 卷挂载到 %s", cfg.StateDir)
	}

	modelTimeout, err := logictryon.LoadModelRequestTimeout(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if msg := logictryon.CheckTimeoutBudget(modelTimeout, LoadShutdownConfig(ctx).Timeout); msg != "" {
		t.Fatalf("超时预算不满足: %s", msg)
	}
	m := regexp.MustCompile(`proxy_read_timeout (\d+)s;`).FindStringSubmatch(tmpl)
	if m == nil {
		t.Fatal("Nginx 模板缺少 proxy_read_timeout")
	}
	readTimeout, _ := strconv.Atoi(m[1])
	if time.Duration(readTimeout)*time.Second < modelTimeout+time.Minute {
		t.Fatalf("proxy_read_timeout %ds 必须 ≥ 模型超时 %s + 60s", readTimeout, modelTimeout)
	}
	sg := regexp.MustCompile(`stop_grace_period: (\d+)s`).FindStringSubmatch(readRepoFile(t, "docker-compose.yml"))
	if sg == nil {
		t.Fatal("docker-compose.yml 缺少 stop_grace_period")
	}
	grace, _ := strconv.Atoi(sg[1])
	sc := LoadShutdownConfig(ctx)
	if time.Duration(grace)*time.Second < sc.DrainDelay+sc.Timeout {
		t.Fatalf("stop_grace_period %ds 必须 ≥ drainDelay + timeout (%s)", grace, sc.DrainDelay+sc.Timeout)
	}
}

// 安全审计回归（F1 / F3 / F7）：Nginx 按路径限制请求体、/tryon 路径变体共用严格限流、
// 兜底 server 拒绝未知 Host、compose 只在 IPv4 上发布端口且请求体临时文件落盘。
func TestNginxComposeSecurityHardening(t *testing.T) {
	compose := readRepoFile(t, "deploy/docker-compose.prod.yml")
	tmpl := readRepoFile(t, "deploy/nginx/templates/nvzhuang.conf.template")

	for _, must := range []string{
		"location ~ ^/tryon/*$ {",
		"listen 80 default_server;",
		"listen 443 ssl default_server;",
		"return 444;",
		"client_max_body_size 64k;",
		"client_max_body_size 30m;",
		"client_header_timeout 15s;",
		"allow 172.30.0.1;",
	} {
		if !strings.Contains(tmpl, must) {
			t.Fatalf("Nginx 模板缺少 %q", must)
		}
	}
	for _, mustNot := range []string{"location = /tryon", "allow 172.30.0.0/24;"} {
		if strings.Contains(tmpl, mustNot) {
			t.Fatalf("Nginx 模板不应包含 %q", mustNot)
		}
	}
	// 30m 只能出现在 /tryon location，server 级不能再有全局大上限
	if n := strings.Count(tmpl, "client_max_body_size 30m;"); n != 1 {
		t.Fatalf("30m 请求体上限只能出现在 /tryon location 中，实际出现 %d 次", n)
	}

	for _, must := range []string{`"0.0.0.0:80:80"`, `"0.0.0.0:443:443"`, "nginx-body-temp:/var/cache/nginx/client_temp"} {
		if !strings.Contains(compose, must) {
			t.Fatalf("生产 compose 缺少 %q", must)
		}
	}
	if regexp.MustCompile(`(?m)^\s+- "(80|443):(80|443)"$`).MatchString(compose) {
		t.Fatal("生产 compose 不应同时在 IPv6 上发布 80/443（请使用 0.0.0.0: 前缀）")
	}
}

// 决策 1：Nginx 在缓冲请求体之前拒绝无 Bearer Token 的 /tryon 请求；
// 401 响应体必须与后端 security.ErrAuthRequired 完全一致，并对 FRONTEND_ORIGIN 带 CORS 头；OPTIONS 预检放行。
// 决策 4：HSTS 带 includeSubDomains。
func TestNginxPreAuthAndHSTS(t *testing.T) {
	compose := readRepoFile(t, "deploy/docker-compose.prod.yml")
	tmpl := readRepoFile(t, "deploy/nginx/templates/nvzhuang.conf.template")
	envExample := readRepoFile(t, "deploy/.env.prod.example")

	e := security.ErrAuthRequired
	body := fmt.Sprintf(`return %d '{"code":%d,"message":"%s","data":{"error_code":"%s","request_id":"$request_id"}}';`,
		e.HTTPStatus, e.BizCode, e.Message, e.ErrorCode)
	if !strings.Contains(tmpl, body) {
		t.Fatalf("Nginx 无 Token 401 响应必须与后端 ErrAuthRequired 一致，期望包含：%s", body)
	}
	for _, must := range []string{
		`"${FRONTEND_ORIGIN}" $http_origin;`,
		`"~*^bearer[ \t]+[^ \t]" 1;`,
		`"OPTIONS:0" 0;`,
		"if ($gw_tryon_missing_bearer) {",
		"add_header Access-Control-Allow-Origin $gw_cors_origin always;",
	} {
		if !strings.Contains(tmpl, must) {
			t.Fatalf("Nginx 模板缺少 %q", must)
		}
	}
	// 预检必须在 /tryon location 内、proxy_pass 之前
	loc := tmpl[strings.Index(tmpl, "location ~ ^/tryon/*$ {"):]
	loc = loc[:strings.Index(loc, "proxy_pass")]
	if !strings.Contains(loc, "if ($gw_tryon_missing_bearer)") {
		t.Fatal("Token 预检必须位于 /tryon location 中 proxy_pass 之前")
	}
	if !strings.Contains(compose, "NGINX_ENVSUBST_FILTER=^(API_SERVER_NAME|TLS_CERT_PATH|TLS_KEY_PATH|FRONTEND_ORIGIN)$") ||
		!strings.Contains(compose, "- FRONTEND_ORIGIN=${FRONTEND_ORIGIN:?") {
		t.Fatal("生产 compose 必须把 FRONTEND_ORIGIN 传给 Nginx 并加入 envsubst 白名单")
	}
	if !regexp.MustCompile(`(?m)^FRONTEND_ORIGIN=https://[a-z0-9.-]+$`).MatchString(envExample) {
		t.Fatal(".env.prod.example 缺少 FRONTEND_ORIGIN 占位")
	}

	hsts := `add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;`
	if strings.Contains(tmpl, `"max-age=31536000" always`) || strings.Count(tmpl, hsts) < 3 {
		t.Fatalf("HSTS 必须统一为 includeSubDomains（server、错误页、401 共 3 处），实际 %d 处", strings.Count(tmpl, hsts))
	}
}

// 决策 4：Docker 基础镜像固定到摘要（tag@sha256），避免同一 tag 被重新发布后构建结果漂移
func TestDockerImagesPinnedByDigest(t *testing.T) {
	pinned := regexp.MustCompile(`^[a-z0-9./_-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$`)
	compose := readRepoFile(t, "deploy/docker-compose.prod.yml")
	for _, m := range regexp.MustCompile(`(?m)^\s+image:\s*(\S+)`).FindAllStringSubmatch(compose, -1) {
		if !pinned.MatchString(m[1]) {
			t.Fatalf("生产 compose 镜像未固定摘要: %s", m[1])
		}
	}
	dockerfile := readRepoFile(t, "backend/manifest/docker/Dockerfile")
	froms := regexp.MustCompile(`(?m)^FROM\s+(\S+)`).FindAllStringSubmatch(dockerfile, -1)
	if len(froms) == 0 {
		t.Fatal("Dockerfile 中没有 FROM")
	}
	for _, m := range froms {
		if !pinned.MatchString(m[1]) {
			t.Fatalf("Dockerfile 基础镜像未固定摘要: %s", m[1])
		}
	}
}
