# AGENTS.md · 协作智能体开发与提交规范

本文档面向所有参与本项目维护的 AI Agent（如 OpenCode、GitHub Copilot、Cursor、Claude Code 等）及协作开发者。任何代理或贡献者在修改、生成代码和提交 Git 记录时，必须严格执行本文档所规定的安全红线与工程规范。

> 阅读顺序：第 1–2 节是不可违反的红线；第 5–9 节是项目现状（目录、工具链、本地测试、安全不变量、配置、前端约定），改动前请先确认所涉及的不变量。

---

## 1. 核心安全红线（零泄露原则）

### 1.1 严禁提交任何真实密钥与凭据
- **绝对禁止**将任何真实的 API Key（包括但不限于 OpenAI `sk-...`、火山方舟 `ARK...`、Google Gemini `AIza...`、通义千问 `sk-...` 等）、私有 Token、密码、证书密钥提交到 Git 仓库或暂存区。
- 仓库内仅允许保留配置文件模板（如 `backend/.env.example`、`backend/manifest/config/config.example.yaml`），且模板内的密钥字段**必须为空或通用占位符**（如 `""`、`"sk-你的key"`、`"你的豆包ArkKey"`）。
- 本地真实配置文件（`backend/manifest/config/config.yaml`、`backend/.env`、根目录 `.env` 等）必须在 `.gitignore` 中被严格忽略。

### 1.2 严禁提交个人隐私与敏感信息
- **绝对禁止**在代码、注释、提交信息（Commit Message）、PR 描述或生成文档中包含个人真实姓名、个人私人邮箱、手机号码、私人局域网 IP、私人服务器公网 IP、个人绝对开发路径（如包含开发者个人用户名的路径）。
- Git 提交者信息应保持项目已有的一致风格，禁止在无授权情况下将私人联系方式作为作者写入共有历史。

### 1.3 外部输入与 URL 参数严格校验
- 任何接受外部 URL、查询参数（如 `?tryon=`）或请求体的逻辑，必须执行严格的来源白名单与安全校验（仅允许同源 Same-Origin 或本机回环 `127.0.0.1`/`localhost`）。
- 绝不允许构造链接时静默接管请求并向未受信任的第三方域外发包含用户私有 API Key（如 `client_config.api_key`）、认证 Token 或用户照片的敏感载荷。
- 非本机外部远程地址一律强制使用 `HTTPS`，杜绝明文 HTTP 传输凭据。

---

## 2. 架构与防御性编码规范

1. **多层兜底保障（Fail-Safe Defaults）**：
   - 后端服务（GoFrame）在启动时，若检测到宿主机未显式创建 `config.yaml`，必须具备代码层自动回退加载 `config.example.yaml` 的能力。
   - 服务端口在未配置时，必须硬兜底显式绑定 `:8000`，绝对禁止因未配置 address 而进入系统 `:0` 随机高位临时端口（如 `:37093`）。
   - Dockerfile 构建阶段与 Compose 编排阶段，必须确保模板配置文件就绪，实现开箱即用。

2. **最小权限与数据隐私**：
   - 前端用户的「个人模型配置」与历史生成记录仅保存在用户浏览器本地（`localStorage` / `sessionStorage`（“仅本次会话”）/ `IndexedDB`），后端在内存中使用，不得向磁盘持久化、不得写入服务端日志。
   - 服务端唯一允许落盘的运行状态是 `security.stateDir` 下的每日额度计数（键为主体 SHA-256 前缀，不含 IP/Token/请求内容）。
   - 用户上传的照片同样只在内存中处理；ComfyUI 模式下上传到 ComfyUI 的 temp 目录并在完成或取消后清理历史，不得写入后端日志或长期目录。

---

## 3. Git 提交自我审查 Checklist（提交前必查）

在任何 `git commit` 执行之前，Agent 必须在后台严格执行以下核验步骤。步骤 1、3、4 已固化为本地脚本 `scripts/check.sh`（任一步失败即非零退出），提交前在仓库根目录运行；步骤 2 与步骤 5 仍需人工逐项确认：

```bash
scripts/check.sh              # 常规检查：禁止文件、敏感串扫描、vet/test/race/govulncheck、全部前端测试、Shell 语法
scripts/check.sh --release    # 发布前：额外 -count=5、构建镜像、trivy/grype 镜像扫描、端到端测试（如已提供 scripts/e2e.sh）
git config core.hooksPath .githooks   # 可选：启用提交前钩子自动运行 check.sh
```

本项目不使用云端 CI，以上检查全部在本机执行。

- [ ] **步骤 1：排查 Untracked 与暂存文件**
  运行 `git status`，确保没有临时文件、日志文件（`*.log`）、编译产物（`main`、`*.exe`）、真实配置文件（`config.yaml`）进入暂存区。
- [ ] **步骤 2：逐行审查 Git Diff**
  运行 `git diff --cached`，逐行检查每一处新增和修改的字符，确认：
  - 无真实 Key、Token、密码；
  - 无个人姓名、电话、私有私人邮箱、个人电脑私有路径；
  - 无调试时遗留的临时硬编码凭证。
- [ ] **步骤 3：敏感字符串安全扫描**
  对修改文件执行正则扫描，确保无符合正则 `(sk-[a-zA-Z0-9]{20,}|AIza[a-zA-Z0-9]{20,})` 的高危私钥。
- [ ] **步骤 4：自动化测试与构建校验**（在 `backend/` 下执行，全部通过才可提交）
  ```bash
  go vet ./...
  go test ./...
  go test -race ./...
  govulncheck ./...                     # go install golang.org/x/vuln/cmd/govulncheck@latest
  node ../tests/frontend_tryon_test.mjs # 修改 index.html 时必跑（在仓库根目录则为 node tests/frontend_tryon_test.mjs）
  node ../tests/search_test.mjs        # 搜索召回与性能回归（scripts/check.sh 会运行 tests/ 下全部 .mjs）
  ```
  涉及测试辅助代码或路由注册时，额外运行 `go test -count=5 ./...` 确认无重复注册等不稳定问题。
- [ ] **步骤 5：文档与配置同步**
  新增或修改配置项、错误码、安全行为时，同步更新 `config.example.yaml`、`backend/DEPLOY.md` 与本文件相关章节；前端提示文案需同时补齐中英文。

---

## 4. Commit Message 提交规范

遵循 **Conventional Commits** 规范，提交信息使用中文，简洁、清晰、明确表达变更动机与影响（可带作用域，如 `fix(tryon): ...`、`feat(security): ...`）：

- `fix: ...`：修复缺陷、修复漏洞或修复配置异常
- `feat: ...`：新增功能或用户可见特性
- `docs: ...`：仅文档调整（README、DEPLOY、规范文档等）
- `test: ...`：新增或重构测试用例
- `refactor: ...`：既不修复错误也不添加新功能的代码重构
- `chore: ...`：构建流程、依赖维护或辅助工具变动

示例：
- `fix: 修复无配置文件时服务监听随机端口与 tryon 参数跨源安全隐患 (close #2, close #3)`
- `docs: 完善部署说明并新增 AGENTS.md 协作规范`

---

## 5. 项目概览与目录

女装穿搭图鉴静态站（纯前端，可直接部署到 GitHub Pages）+ 可选的 AI 换装后端（GoFrame v2.10.4）。

```
nvzhuang/
├── index.html                 # 单文件前端：页面、样式、交互、中英文字典 I18N、AI 试穿与设置中心、CSP meta
├── data.js                    # 服装数据（META + D 数组：id / 分类 / 关键词 / 图片路径）
├── assets/                    # thumbs/、img/ 图片资源
├── tests/frontend_tryon_test.mjs  # 前端 Node 测试（从 index.html 抽取函数在 vm 中断言）
├── tests/search_test.mjs          # 搜索召回/性能回归（中文子串、英文与 ID 前缀）
├── scripts/check.sh           # 本地质量门禁（AGENTS.md §3），.githooks/pre-commit 可选调用
├── docker-compose.yml         # 后端编排（仅绑定 127.0.0.1:8000、read_only、cap_drop）
├── docs/                      # 审计结论、发布清单、需求与技术文档
└── backend/
    ├── main.go                # 入口 → internal/cmd
    ├── api/{hello,tryon}/v1   # 请求/响应结构与路由元数据（/hello、POST /tryon、GET /comfyui/health；/healthz、/readyz 在 cmd/probes.go）
    ├── internal/cmd           # 启动：配置回退、:8000 兜底、RegisterRoutes 中间件装配
    ├── internal/controller    # 控制器：参数与图片校验、并发槽与每日额度（调用上游前失败会退还）
    ├── internal/logic/tryon   # 模型适配（openai 兼容/gpt-image、doubao、gemini、comfyui）、提示词、netguard.go 出站安全
    ├── internal/middleware    # RequestID、HandlerResponse、CORS、BodyLimit、Auth、RateLimit、ClientIP、路径规范化
    ├── internal/security      # 安全配置、Token 存储、限流/配额/并发、标准错误码、日志脱敏 RedactSecrets
    ├── internal/imagevalidate # Data URL 解析与图片真实格式/尺寸/像素/大小校验
    ├── internal/lifecycle     # /healthz、/readyz 就绪检查项与优雅停机参数（cmd.Serve 负责信号与停机流程）
    ├── manifest/config        # config.example.yaml（入库模板）；config.yaml（本地，已忽略）
    ├── manifest/docker/Dockerfile  # golang:1.26-alpine 构建，alpine:3.22 运行，非 root、HEALTHCHECK
    ├── workflows/             # ComfyUI API 工作流 JSON
    └── DEPLOY.md              # 部署与安全说明（Nginx、trustedProxies、数据流向）
```

## 6. 工具链与本地运行

- Go：`go 1.26`（`backend/go.mod`；x/net 等依赖要求 1.26），Docker 构建镜像 `golang:1.26-alpine`。
- 运行后端：`cd backend && go run .`；无 `config.yaml` 时自动加载 `config.example.yaml`，未配置地址时监听 `:8000`。`security.authRequired: true`（默认）时必须在 `TRYON_API_KEYS` 中提供 32–512 字符的随机 Token（如 `openssl rand -hex 32`）。
- 运行前端：仓库根目录起任意静态服务（如 `python3 -m http.server 3000`），默认 CORS 白名单包含 `http://localhost:3000` 与 `http://127.0.0.1:3000`。
- 测试命令见第 3 节步骤 4。Fuzz 目标（按需短时运行，例如 `go test -run '^$' -fuzz '^FuzzNormalizePath$' -fuzztime 20s ./internal/middleware`）：
  - `internal/middleware`：`FuzzNormalizePath`、`FuzzSanitizeRequestID`
  - `internal/logic/tryon`：`FuzzValidateClientModelConfig`
  - `internal/security`：`FuzzRedactSecrets`
  - `internal/imagevalidate`：`FuzzValidateImage`
  - fuzz 生成的 `testdata/fuzz/` 语料默认不入库（可能含形似密钥的字符串，会触发提交前扫描）；需要固化的用例改写为普通单元测试。
- 测试约定：`g.Server(name)` 是按名称缓存的单例，测试中必须用 `uniqueServerName(...)`（见 cmd、controller/tryon、middleware 包的 servername_test.go）生成唯一名称；配置相关测试用 `gcfg.NewAdapterContent` 临时替换并在结束时恢复适配器。

## 7. 安全架构与不可回退的不变量

以下行为均有回归测试覆盖，修改相关代码时不得削弱：

1. **路由与中间件**：受保护接口（`/tryon`、`/comfyui/health`）在 `cmd.RegisterRoutes` 中以路由组挂载 `BodyLimit → Auth → RateLimit`，不依赖 `r.URL.Path` 精确匹配；需要按路径判断时一律先 `middleware.NormalizePath`。`//tryon`、`/tryon/` 等写法必须同样被拦截。外层组依次为 `RequestID → HandlerResponse → CORS`。
2. **SSRF 防护（`logic/tryon/netguard.go`）**：浏览器传入的 `client_config` 视为不可信——Base URL 必须 HTTPS、无用户信息，端口在 `security.clientModelAllowedPorts`（默认 `[443]`）内；拒绝 localhost 与 IANA 特殊用途地址全表（含 CGNAT/云元数据 `100.100.100.200`、`169.254/16`、`198.18/15`、NAT64、6to4、IPv4 映射地址等），并在**拨号阶段**对 DNS 解析结果再次校验（防 DNS rebinding），不走系统代理。
3. **结果图片下载**：上游返回的 URL 默认按不可信处理（仅 HTTPS 公网 443）；仅当模型来自服务端配置、且结果地址与其 `baseURL` 同源时才使用可信客户端（允许内网/http/非 443）。下载不携带任何鉴权头，限制大小与重定向次数。
4. **重定向与响应体**：模型接口只允许同协议同主机重定向（最多 3 次，防 `x-goog-api-key` 等外泄）；上游响应体限量读取（生成结果 64 MiB、视觉 1 MiB、错误体 4 KiB）。
5. **远程地址 HTTPS**：服务端与前端对非本机地址一律要求 HTTPS；只有本机回环允许 http。
6. **客户端 IP**：限流/配额默认只用 TCP 对端地址；仅当对端属于 `security.trustedProxies` 时，才从 `X-Forwarded-For` 自右向左取第一个非可信地址（其次 `X-Real-IP`）。
7. **错误处理**：上游原始错误绝不回显，经 `security.RedactSecrets` 脱敏后写服务端日志；对外返回固定文案 + `request_id`（`X-Request-ID` 仅接受 1–64 位 `[A-Za-z0-9_-]`，否则重新生成）。HTTP 状态码按真实类型返回：400 参数/配置校验、401 鉴权、403 来源不允许、413 请求体过大、422 图片校验失败、429 限流/额度/并发、500 内部错误、502 上游失败（`UPSTREAM_FAILED`）、503 模型未配置/不支持。参数类错误使用 `gerror.NewCode`（不要用会带 “Invalid Parameter:” 前缀的 `WrapCode(..., "")`）。
8. **Token**：只保存 SHA-256 并恒定时间比较；启动时拒绝示例/占位与低熵 Token，`authRequired: true` 且无有效 Token 时启动失败。
9. **CORS**：`security.trustedOrigins` 精确匹配白名单，禁止 `*`，响应带 `Vary: Origin`。
10. **暴露面**：Swagger/OpenAPI 默认关闭；compose 仅绑定 `127.0.0.1:8000`，容器非 root、只读根文件系统；保留 `config.example.yaml` 回退与 `:8000` 兜底。
11. **探针与停机**：`/healthz`、`/readyz` 不鉴权，因此绝不调用付费上游、不返回错误细节；停机由 `cmd.Serve` 处理（readyz 先失败 → drainDelay → 等待在途请求最多 `shutdown.timeoutSeconds`），不得改回 GoFrame 默认信号处理（会中断在途请求）。

## 8. 关键配置项（`backend/manifest/config/config.example.yaml`）

- `security.authRequired` / `apiKeysEnv`（默认 `TRYON_API_KEYS`）/ `trustedOrigins` / `allowPrivateNetwork` / `trustedProxies` / `clientModelAllowedPorts`。
- `security.rateLimit`：`requestsPerMinute`、`burst`、`limiterIdleMinutes`、`dailyRequestsPerPrincipal`；`security.stateDir`：每日额度 bbolt 持久化目录（空 = 纯内存）；只落盘主体哈希 + 日期 + 次数，目录不可用时启动失败，运行期出错降级为内存并使 `/readyz` 的 `state` 失败。
- `security.concurrency`：`global`、`perPrincipal`、`queueTimeoutSeconds`（默认 0 不排队，最大 60；排队超时与并发拒绝返回 429 + `Retry-After: 5`）。
- `llm.requestTimeoutSeconds`：云端模型 HTTP 总超时（默认 180，10–1800）；必须小于反向代理读超时（Nginx 建议 300s）且比 `shutdown.timeoutSeconds` 至少小 5 秒。
- `shutdown.drainDelaySeconds`（默认 3）/ `shutdown.timeoutSeconds`（默认 190，须大于模型请求超时）。
- `upload.*`：请求体上限（默认 24 MiB，超限统一 413 JSON）、单图编码/解码大小、合计大小、宽高与像素上限、允许 MIME（jpeg/png/webp）。
- `llm.models.<name>`：`provider`（openai/doubao/gemini/comfyui）、`apiKey` 或 `envKey`、`baseURL`、`model`、ComfyUI 的 `workflowPath`。模板中的密钥必须为空或占位。

## 9. 前端约定（`index.html`）

- 所有用户可见文案放入 `I18N.zh` / `I18N.en` 并同时补齐；后端固定错误码（AUTH_*、REQUEST_TOO_LARGE、IMAGE_*、RATE_LIMITED、UPSTREAM_FAILED、INTERNAL_ERROR 等）在 `getErrorMessageFromResponse` 中映射为本地化文案，仅 400 校验类错误展示后端具体原因。
- `?tryon=` 仅接受同源或本机回环地址（`parseSafeTryonParam`），跨源一律忽略；设置中心手动填写的后端地址允许本机回环或远程 HTTPS，校验需与后端一致；个人模型（非 ComfyUI）Base URL 必须是公网 HTTPS（`validCustomModelBaseUrl`）。`client_config`（含用户 API Key）只发往受信任目标（设置中的后端、本机回环或同源）。
- 收到 413 时提示用户并自动勾选“上传前压缩”，由用户重新提交（不自动重发）。
- 插入 HTML 前必须经 `esc()` 转义；遵守页面 CSP，不引入外部脚本。

## 10. 审计历史与参考文档

- 2026-10-30：深度代码审计，发现 27 项（1 严重、2 高危），修复后合并为 PR #1。
- 2026-10-31：合并后复审，修复结果图下载回归、新增 `clientModelAllowedPorts`、日志脱敏绕过、错误前缀、前端错误映射与 `-count` 测试不稳定，合并为 PR #2。
- 参考：`backend/DEPLOY.md`（部署、Nginx、安全与数据流向）、`docs/LINUXDO_RELEASE_AUDIT.md`（审计结论）、`docs/LINUXDO_PUBLISH_CHECKLIST.md`、`docs/2026-10-30_security-image-validation-requirements.md`、`README.md`。
