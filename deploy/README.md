# deploy/ · 生产部署叠加配置

面向单台海外 VPS（推荐 4c24g，仅用云端模型、不部署 ComfyUI/GPU）的生产配置。所有域名、路径、网段之外的真实值都由你在本机填写，**仓库里只保留占位值**。

> 首次装机请直接按 [`FIRST-INSTALL.md`](FIRST-INSTALL.md) 从上到下执行（可整份交给 AI 执行，含输入清单、每步预期输出、验收与完成定义）；上线验收脚本为 [`verify.sh`](verify.sh)。

```
deploy/
├── docker-compose.prod.yml          # 叠加在根目录 docker-compose.yml 之上：Nginx 容器 + 固定内部网络，后端不发布端口
├── .env.prod.example                # 域名、证书路径、配置文件路径的占位模板（复制为 .env.prod，已被 .gitignore 忽略）
├── backend/config.prod.overrides.yaml  # 生产 config.yaml 需要覆盖的键（文档性片段，合并进你的 config.yaml）
├── nginx/templates/nvzhuang.conf.template  # Nginx 站点模板（官方镜像启动时 envsubst）
├── nginx/errors/*.json              # 网关自身返回的 413/429/502/504 JSON 错误页
├── nginx/smoke-test.sh              # 本机 Nginx 模板冒烟测试（不需要 Docker、不访问外网）
├── host/init-host.sh                # 主机初始化（默认只打印计划，--apply 才执行）
├── verify.sh                        # 上线验收：HTTPS/安全头/鉴权/CORS/SSRF/413/429，--local 追加容器与日志检查，--restart 追加优雅重启
└── FIRST-INSTALL.md                 # 首次装机运行手册（交付 AI 逐步执行）
```

## 拓扑

```
浏览器 ──HTTPS──> Nginx 容器 172.30.0.2 (:80/:443) ──HTTP──> 后端容器 172.30.0.10:8000 ──HTTPS──> 云端模型
前端静态站（GitHub Pages 等）只需把后端地址设为 https://<你的 API 域名>
```

## 两个上线风险（已有测试守护）

1. **trustedProxies 与 Docker 网桥**：容器里看到的对端地址是 Nginx 容器地址，而不是 127.0.0.1；如果有人绕过 Nginx（例如端口仍被发布），对端会是 Docker 网桥网关。因此：
   - 内部网络固定为 `172.30.0.0/24`，后端 `security.trustedProxies` 只写 `["172.30.0.2/32"]`；
   - 不要写 `127.0.0.1`、网关 `172.30.0.1` 或整个网段；
   - 后端若收到来自**不可信私网/回环对端**且带 `X-Forwarded-For` / `X-Real-IP` 的请求，会每 10 分钟最多记录一次告警（只记录对端地址），提示检查此项。
2. **Nginx 读超时与模型超时**：`proxy_read_timeout 300s` 必须大于 `llm.requestTimeoutSeconds`（180）+ 60 秒余量；`shutdown.timeoutSeconds`（190）≥ 模型超时 + 5 秒；根 compose 的 `stop_grace_period`（200s）≥ drainDelay + 停机超时。

3. **IPv6 与未知 Host**：compose 只以 `0.0.0.0:80`、`0.0.0.0:443` 发布端口。Docker 默认网络没有 IPv6 地址，IPv6 入站会经 docker-proxy 转成网关 `172.30.0.1`，所有 IPv6 用户将共用一个限流桶、并命中探针白名单；因此不发布 IPv6，探针白名单也只保留 `127.0.0.1`、`::1` 与宿主机本机访问时的 `172.30.0.1`。80/443 另有 `default_server` 兜底：Host 或 SNI 不是 API 域名（按 IP 扫描、伪造 Host）时直接断开（444），443 兜底只放行容器自检 `https://127.0.0.1/healthz`。
4. **请求体**：全局 `client_max_body_size 64k`，只有 `location ~ ^/tryon/*$`（覆盖 `/tryon/`、`//tryon`、`/tryon%2F` 等写法，共用严格限流）放宽到 30m；超过内存缓冲的部分写入磁盘卷 `nginx-body-temp`，不计入 Nginx 容器的 256 MiB 内存上限。
5. **无 Token 预检**：`/tryon` 未带 `Authorization: Bearer <非空>` 时，Nginx 在 rewrite 阶段直接返回与后端一致的 `401 {"code":40101,...,"error_code":"AUTH_REQUIRED"}`，不读取、不缓冲请求体（超过 30m 的 Content-Length 仍先返回 413）；OPTIONS 预检照常转发给后端。Nginx 只检查格式，Token 真伪仍由后端校验。该 401 与 JSON 错误页只对 `.env.prod` 中的 `FRONTEND_ORIGIN` 带 `Access-Control-Allow-Origin`（精确匹配），因此 `FRONTEND_ORIGIN` 必须与 `config.yaml` 的 `security.trustedOrigins` 一致，否则前端会显示网络错误而不是“请填写 Token”。无 Token 请求在限流之前返回，不消耗 `/tryon` 限流桶。
6. **HSTS 含 `includeSubDomains`**：`Strict-Transport-Security: max-age=31536000; includeSubDomains`。浏览器会在一年内对 **API 域名的所有子域名**强制 HTTPS——如果 `API_SERVER_NAME` 下还有只提供 HTTP 的子域名（如 `x.api.example.com`），启用前先为它们配好 HTTPS，或改用独立的 API 域名。HSTS 只对 API 域名本身下发，不影响父域名或前端域名。
7. **镜像按摘要固定**：`nginx:1.28-alpine`、`golang:1.26-alpine`、`alpine:3.22` 均写成 `tag@sha256:<多架构 index 摘要>`，同一 tag 被重新发布也不会改变构建结果。定期运行 `scripts/image-digests.sh`（只读，访问 Docker Hub）检查是否有新构建；出现 `CHANGED` 时把新摘要写回文件、重新构建并验收后提交。`TestDockerImagesPinnedByDigest` 保证不会退回裸 tag。

`backend/internal/cmd/deploy_consistency_test.go`（`TestProdOverridesConsistency`、`TestNginxComposeSecurityHardening`、`TestNginxPreAuthAndHSTS`、`TestDockerImagesPinnedByDigest`）会交叉校验 compose、Nginx 模板与覆盖配置，任何一处改动不一致都会让 `scripts/check.sh` 失败。

## 使用步骤（在仓库根目录）

```bash
# 1. 主机初始化（先看计划，确认无误后再 --apply；SSH_ALLOW_CIDR 填你自己的管理网段）
sudo ADMIN_USER=<你的管理用户> SSH_ALLOW_CIDR=<管理网段/掩码> deploy/host/init-host.sh
sudo ADMIN_USER=<你的管理用户> SSH_ALLOW_CIDR=<管理网段/掩码> deploy/host/init-host.sh --apply

# 2. 准备配置：参照 deploy/backend/config.prod.overrides.yaml 修改 config.yaml（容器以 UID/GID 10001 读取，权限 root:10001 0640）；
#    Token 与模型 Key 只写进根目录 .env（0600，不提交；不要写在 backend/.env，原因见下）
cp backend/manifest/config/config.example.yaml backend/manifest/config/config.yaml
cp deploy/.env.prod.example deploy/.env.prod

# 3. 校验并启动（需要 Docker Compose v2.24+）。两个 --env-file 缺一不可，且根目录 .env 必须在前
dc() { docker compose -p nvzhuang --env-file .env --env-file deploy/.env.prod -f docker-compose.yml -f deploy/docker-compose.prod.yml "$@"; }
dc config --quiet
dc up -d

# 4. 验收（Token 从文件读取，不会打印）
API_DOMAIN=<API 域名> FRONTEND_ORIGIN=<前端来源> TRYON_TOKEN_FILE=<只含一个 Token 的文件> VERIFY_CONNECT_IP=127.0.0.1 sudo -E deploy/verify.sh --local
```

> **为什么必须写两个 `--env-file`**：根 `docker-compose.yml` 的 `environment:` 中 `OPENAI_API_KEY=${OPENAI_API_KEY:-}` 等四个 Key 由 compose 插值得到，且优先级高于 `env_file`。只传 `--env-file deploy/.env.prod` 时 compose 不再读取根目录 `.env` 做插值，这些 Key 会被插值成空串并覆盖 `env_file` 中的真实值，`/readyz` 的 `models` 随之失败。

证书：首次签发时 Nginx 尚未启动（模板启动时就需要证书），先用 `certbot certonly --standalone`，服务启动后再切换为 webroot 续期（webroot 目录即 `ACME_WEBROOT`，建议写绝对路径；Nginx 在 80 端口提供 `/.well-known/acme-challenge/`），完整步骤见 [`FIRST-INSTALL.md`](FIRST-INSTALL.md) §7、§9.2。

## 网关错误码

Nginx 自身拦截的请求返回与后端同结构的 JSON（`deploy/nginx/errors/`），前端 `getErrorMessageFromResponse` 已映射：

| HTTP | code | error_code | 场景 |
|---|---|---|---|
| 413 | 41300 | REQUEST_TOO_LARGE | 请求体超过 `client_max_body_size`（`/tryon` 30m，其它路径 64k） |
| 429 | 42900 | RATE_LIMITED | Nginx `limit_req` / `limit_conn` 限流（附 `Retry-After: 10`） |
| 502/503 | 50200 | GATEWAY_ERROR | 后端不可达 |
| 504 | 50400 | GATEWAY_TIMEOUT | 超过 `proxy_read_timeout` |

后端自己返回的 JSON 错误（如 42903 并发限制）原样透传（`proxy_intercept_errors off`）。

## 注意

- Docker 发布的端口会绕过 ufw 规则：后端端口已用 `ports: !reset []` 清除，只有 Nginx 的 80/443 对外；不要再给后端加端口映射。
- `/healthz`、`/readyz` 在 Nginx 上只允许容器内与宿主机本机访问，外部监控请通过 SSH 隧道或在主机上执行。
- API 域名只配 A 记录、不要配 AAAA：端口只在 IPv4 上发布，有 AAAA 时 IPv6 客户端会先连接失败再回退。若确需 IPv6，必须先给 compose 网络启用 IPv6（`enable_ipv6` + Docker `ip6tables`）让 Nginx 看到真实地址，再改端口发布并删除探针白名单中的 `172.30.0.1`。
- 本地验证：`NGINX_BIN=/path/to/nginx deploy/nginx/smoke-test.sh`（以 root 运行，或非 root 但系统支持用户命名空间 `unshare -rn` 时，还会对生产模板执行 `nginx -t`）。
