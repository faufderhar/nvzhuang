# AI 换装后端部署指南

本文档说明如何把 `backend/`（GoFrame v2 后端）部署到**你自己的服务器**。

## 一、前置条件

- 一台能访问对应服务商的服务器（调国外模型需能访问 OpenAI / Google，用豆包等国内模型则只需能访问国内网络即可）
- 服务器已安装 Go 1.26+（`go.mod` 要求 go 1.26.0，依赖的安全版本需要）或 Docker
- 已申请 API Key（按需，用哪个模型就配哪个；若使用本地 ComfyUI 则完全不需要申请 Key）：
  - OpenAI GPT-Image：`sk-...`（https://platform.openai.com）
  - 豆包（火山方舟）：`ARK...`（https://console.volcengine.com/ark）
  - Google Gemini：`AIza...`（https://aistudio.google.com）
  - 其他：通义万相、智谱 CogView、硅基流动等 OpenAI 兼容服务均可接入
  - ComfyUI 本地离线部署：若主机拥有独立显卡，支持直接接入本地 ComfyUI（优先 Qwen-Image-Edit-2511 4步极速生图，兼容 MiniMax H3），无需任何云端 API 费用。详见 `README.MD`。

## 二、推荐部署方式（优先 Docker）

### 方式 A：Docker Compose（首选推荐，最省事）

```bash
cd backend

# 1. 复制环境变量示例文件
cp .env.example .env

# 2. 编辑 .env 文件，填入你使用的模型 API Key（按需填写，国内优先推荐豆包 ARK_API_KEY）
# 例如：ARK_API_KEY=xxxx

# 3. 一键构建并启动后台容器（内置开箱即用自动兜底，默认监听 8000 端口）
docker compose up -d

# 查看日志
docker compose logs -f
```

> 说明：系统默认自动加载配置模板并绑定 `:8000` 端口。若需深度定制模型或路由，可复制配置：`cp manifest/config/config.example.yaml manifest/config/config.yaml`。
>
> 容器安全说明：镜像以非 root 用户（UID 10001）运行；`.dockerignore` 会排除 `config.yaml` 与 `.env`，自定义配置请按 `docker-compose.yml` 中注释以只读卷挂载，不要打进镜像。compose 默认只绑定 `127.0.0.1:8000`，并启用 `read_only`、`cap_drop: [ALL]` 与健康检查。容器内的 `127.0.0.1` 指向容器自身，访问宿主机 ComfyUI 请把 `baseURL` 改为 `http://host.docker.internal:8188`。

### 方式 B：Docker Run 单命令运行

```bash
# 1. 在 backend 目录构建镜像
cd backend
docker build -f manifest/docker/Dockerfile -t costume-tryon .

# 2. 运行容器，通过环境变量注入 Key（按需填写，豆包用 ARK_API_KEY）
# 端口只绑定本机回环，由 Nginx 对外；以只读根文件系统、无 capability 方式运行
# 如需自定义配置：-v "$PWD/manifest/config/config.yaml:/app/manifest/config/config.yaml:ro"
docker run -d --name costume-tryon \
  -p 127.0.0.1:8000:8000 --stop-timeout 200 \
  --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges \
  --restart unless-stopped \
  -e OPENAI_API_KEY="sk-你的key" \
  -e ARK_API_KEY="你的豆包ArkKey" \
  -e GEMINI_API_KEY="AIza你的key" \
  costume-tryon
```

### 方式 C：直接编译运行（无 Docker）

```bash
cd backend

# 编译 Linux 可执行文件（如在本机 Windows 交叉编译）
GOOS=linux GOARCH=amd64 go build -o main .

# 上传 main 到服务器，设置环境变量后运行
export OPENAI_API_KEY="sk-你的key"
export ARK_API_KEY="你的豆包ArkKey"
export GEMINI_API_KEY="AIza你的key"
./main
```

> 服务默认监听 `:8000`。可用环境变量 `GF_GCFG_FILE` 指定配置文件路径，或用 `--gf.gcfg.file` 参数。

## 二点五、模型与 Key 配置说明

所有模型的 **API Key、Base URL、模型名** 都在 `manifest/config/config.yaml` 的 `llm.models` 下配置，均可自由修改：

```yaml
llm:
  models:
    gpt-image:            # 前端请求用的别名
      provider: openai    # 协议：openai（OpenAI Images 兼容）/ gemini
      apiKey: ""          # 直接填 Key，或留空用环境变量
      envKey: "OPENAI_API_KEY"   # 环境变量名（优先于 apiKey）
      baseURL: "https://api.openai.com/v1"
      model: "gpt-image-2"
    doubao:               # 豆包（火山方舟），国内可用
      provider: doubao    # 火山方舟协议（复用 images/generations，但请求体为火山格式）
      envKey: "ARK_API_KEY"
      baseURL: "https://ark.cn-beijing.volces.com/api/v3"
      model: "doubao-seedream-3-0-t2i-250415"
    gemini:
      provider: gemini
      envKey: "GEMINI_API_KEY"
      baseURL: "https://generativelanguage.googleapis.com/v1beta"
      model: "gemini-3.1-flash-image"
```

**接入任意 OpenAI 兼容的国内生图模型**（通义万相、智谱 CogView、硅基流动、各类中转站），只需新增一个条目：

```yaml
    qwen:                 # 通义万相示例
      provider: openai
      envKey: "DASHSCOPE_API_KEY"
      baseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"
      model: "wanx2.1-t2i-turbo"

    comfyui:              # 本地 ComfyUI 离线方案（默认 Qwen 优先，自动回退 H3）
      provider: comfyui
      baseURL: "http://127.0.0.1:8188"
      model: "qwen-image-edit-2511"
      workflowPath: "workflows/comfyui_qwen_image_edit_2511_api.json"
```

> 关键点：
> - `provider: openai` 表示走标准的 `POST {baseURL}/images/generations` + `input` 数组协议（OpenAI 官方 GPT-Image、通义万相、智谱 CogView、多数中转站）。把 baseURL 换成服务商地址、model 换成其模型 ID 即可。
> - `provider: doubao` 走火山方舟协议（同样是 `/images/generations`，但请求体用 `prompt` + `image[]` 多图生图，返回 `url`），适配豆包 Seedream 换装。
> - `provider: gemini` 走 Google 原生 `generateContent` 协议。
> 前端模型下拉框在 `index.html` 的 `<select id="aiModel">` 中增减 `<option>` 即可与后端别名对应。

## 三、配置前端指向后端

前端 `index.html` 按以下优先级获取后端地址：

1. **设置中心**：用户在当前浏览器保存的后端 API 地址（公网外部地址须使用 `HTTPS`）
2. **全局变量**：在页面加载前设置 `window.AI_API_URL = "https://api.你的域名"`
3. **URL 参数**：`https://你的前端/?tryon=http://127.0.0.1:8000`（安全约束：为防止跨域钓鱼窃取 API Key 与私密照片，`?tryon=` 仅允许同源地址或本机回环 `localhost/127.0.0.1`；外部后端须在设置中心显式保存）
4. **本地自动回退**：访问宿主为 `localhost` 或 `127.0.0.1` 时，自动尝试回退连接 `http://127.0.0.1:8000`

> 建议：若部署公网，把后端反向代理到 `https://api.你的域名`（Nginx 配置见下），前端部署时在 `index.html` 顶部加一句：
> ```html
> <script>window.AI_API_URL="https://api.你的域名";</script>
> ```

## 四、Nginx 反向代理（可选但推荐）

用 HTTPS 域名 + 反代，避免前端跨域和明文 Key 问题：

```nginx
server {
    listen 443 ssl;
    server_name api.你的域名.com;

    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    # 合并重复斜杠（//tryon → /tryon），与后端的路径规范化保持一致
    merge_slashes on;

    # 大图片 base64 请求体：需 ≥ 后端 security.maxRequestBodyBytes（默认 24 MiB）
    client_max_body_size 30m;

    location / {
        proxy_pass http://127.0.0.1:8000;
        proxy_set_header Host $host;
        # 用真实连接地址覆盖（而不是追加）客户端传入的 X-Forwarded-For / X-Real-IP，防止伪造 IP 绕过限流
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 180s;
        proxy_send_timeout 180s;
    }
}
```

后端默认**只信任 TCP 连接的对端地址**，忽略 `X-Forwarded-For`。放在 Nginx 之后时，需要在 `config.yaml` 中声明可信代理，否则所有用户会共用 Nginx 的 IP 做限流与配额：

```yaml
security:
  trustedProxies: ["127.0.0.1/32", "::1/128"]   # 同机 Nginx；Docker 网络内的代理请填写其网段，如 172.18.0.0/16
```

仅当请求来自 `trustedProxies` 中的地址时，后端才会从 `X-Forwarded-For` 中取最右侧的不可信地址作为客户端 IP。不要把 `0.0.0.0/0` 写进去。

## 五、安全与成本控制防护（公网部署必读）

针对 `/tryon` 换装接口，后端已原生集成**访问鉴权、CORS 精确白名单、速率限制、每日配额、并发控制与图片安全校验**，防止大模型账单被恶意刷取或服务器资源耗尽：

### 1. 访问凭证鉴权（Bearer Token）
- **开启鉴权**：在 `manifest/config/config.yaml` 中配置 `security.authRequired: true`（本地调试可临时设为 `false`，公开部署必须为 `true`）。
- **配置密钥**：在环境变量或 `.env` 文件中设置 `TRYON_API_KEYS`（多个 Token 用英文逗号分隔，每个 Token 须为 32 到 512 个字符）。
  ```bash
  # 每个客户端单独生成一个随机 Token
  TRYON_API_KEYS="$(openssl rand -hex 32),$(openssl rand -hex 32)"
  ```
- **拒绝示例值**：后端启动时会拒绝明显的示例/占位 Token（包含 `your-`、`example`、`placeholder`、`changeme` 等片段，或不同字符少于 10 种），请勿直接复制文档中的写法。
- **脱敏安全存储**：后端仅在内存保存 Token 的 SHA-256 哈希摘要，比对使用恒定时间比对（Constant-Time Compare）防止时序侧信道攻击；日志、错误响应与响应头中绝不打印任何 Token 明文。
- **前端配置**：用户可在前端「设置中心」的「试穿后端」区域填入「后端访问 Token」（与第三方模型 API Key 完全隔离独立存储）。发起请求时，前端自动在 Header 中注入 `Authorization: Bearer <token>`。

### 2. 严格的 CORS 跨域来源白名单
- `security.trustedOrigins` 必须配置精确来源域名（如 `https://paipiange.github.io`、`http://localhost:3000`），禁止配置通配符 `*`。
- 未授权来源发起跨域请求将直接返回 HTTP 403（业务错误码 `40301`，`ORIGIN_NOT_ALLOWED`）。

### 3. 频控限流、每日额度与并发锁定
- **令牌桶速率限制**（默认 10 次/分，突发 3 次）：超出限制时立即拒绝并返回 HTTP 429（`42901`，`RATE_LIMITED`），并在响应头附带 `Retry-After` 秒数。
- **每日生成额度**（默认每个调用方 100 次/天）：额度按 UTC 自然日重置；仅当请求通过全部鉴权、图片校验并成功占用并发槽位后才原子扣减，模型/参数配置错误（未调用上游）会退还额度；额度耗尽返回 HTTP 429（`42902`，`DAILY_QUOTA_EXCEEDED`）。*(注：首版配额计数器保存在进程内存中，后端重启会重置)*。
- **并发控制**（全局最大 2 并发，单主体最大 1 并发）：超过并发上限时立即返回 HTTP 429（`42903`，`CONCURRENCY_LIMITED`），不排队阻塞服务器连接。

### 4. 请求体与图片安全限制
- **请求体体积**：HTTP 请求体上限默认限制为 24 MiB（`maxRequestBodyBytes: 25165824`），超限在 JSON 解析前直接截断返回 HTTP 413（`41301`，`REQUEST_TOO_LARGE`）。
- **真实文件格式**：后端使用内容嗅探及图像解码器严格检测真实 MIME，仅允许 `image/jpeg`、`image/png`、`image/webp`，禁止 SVG、GIF、可执行文件等格式（`42202`，`UNSUPPORTED_IMAGE_TYPE`）。
- **图片尺寸与像素**：
  - 单图解码后体积上限 8 MiB，两图（人像+服装）合计上限 16 MiB（`42203`，`IMAGE_FILE_TOO_LARGE`）。
  - 宽、高必须在 256 px 至 8192 px 之间（`42204`，`IMAGE_DIMENSIONS_INVALID`）。
  - 总像素乘积不超过 20,000,000 px（防止超大图像解压导致的内存耗尽炸弹，`42205`，`IMAGE_PIXEL_LIMIT_EXCEEDED`）。
- **前端自动压缩**：前端在上传与发送前自动进行阶梯压缩，最长边自适应缩小到 2048 px 内，优先采用 WebP（质量 0.90），降级 JPEG（0.90~0.75），确保绝大多数手机实拍照片无感顺畅上传。

### 5. API Key 隔离与保护
- Key 仅保存在服务器本地环境变量或 `.env`，禁止将包含真实 Key 的 `config.yaml` 或 `.env` 提交到公开仓库。
- 默认的「服务器预设」模式下，前端不接触服务器 API Key，所有大模型通信均由后端代理中转。
- 可选的「个人模型配置」由用户主动保存在自己的浏览器，并只随单次 `/tryon` 请求发送。后端不写数据库、配置文件或业务日志；Gemini Key 使用请求头传递，不进入上游 URL。
- 个人 Base URL 必须为 `https://`、端口默认只能是 443（可通过 `security.clientModelAllowedPorts` 追加，如 `[443, 8443]`），不允许用户名密码、查询参数与片段；拒绝 localhost、内网、CGNAT（含云厂商元数据 100.100.100.200）、NAT64/6to4 等全部 IANA 特殊用途地址，并在建立连接时对 DNS 解析结果再次校验，避免公开后端被滥用为 SSRF 代理。
- 上游重定向：模型接口只允许同协议同主机的重定向（最多 3 次），防止 `x-goog-api-key` 等自定义头被转发到其他主机；结果图片下载只允许 HTTPS。
- 结果图片地址：浏览器自定义模型返回的结果 URL 一律按不可信处理（仅 HTTPS 公网地址）；服务端 `config.yaml` 中配置的模型若返回与其 `baseURL` **同源**（协议、主机、端口均相同）的结果地址，则视为可信，允许是内网、`http://` 或非 443 端口的自建中转站；其它来源的结果地址仍按不可信处理。下载结果图片时不携带任何鉴权头。
- 上游响应体积：模型响应上限 64 MiB，错误响应只读取前 4 KiB，视觉分析响应上限 1 MiB。

### 6. 错误响应与日志脱敏
- 上游模型失败统一返回 HTTP 502（`50201`，`UPSTREAM_FAILED`，“换装生成失败，请稍后重试”）并附带 `request_id`，不回显上游原始响应。
- 参数校验错误返回 HTTP 400，未分类的内部错误返回 HTTP 500 固定文案；错误响应均为 JSON `{code, message, data}`。
- 服务端日志只记录脱敏后的失败原因：Key（`sk-`、`AIza`、Bearer 等）、URL、IP 与长 base64 数据会被替换，并按 `request_id` 关联排查。
- `X-Request-ID` 仅接受 1~64 位 `[A-Za-z0-9_-]`，否则由服务端重新生成。

### 7. 数据流向说明
- 「服务器预设」模式：用户照片只发送给 `config.yaml` 中配置的模型服务。
- 「个人模型配置」模式：照片与 Key 只发送给用户自己填写的 Base URL。
- **GPT Image 系列额外的视觉分析**：模型名或别名包含 `gpt-image` 时，后端会先把人像与服装图发送到**同一 Base URL** 下的轻量视觉模型（官方 OpenAI 为 `gpt-4o-mini`，其他中转站为 `gemini-3.5-flash-lite`）提取文字特征，再调用生图接口。因此同一张照片会产生两次上游调用，请确认你的服务商允许并知悉该用途。
- 本地 ComfyUI：照片上传到 ComfyUI 主机（见下一节的清理说明）。

### 8. ComfyUI 隐私与清理
- 用户图片以 `type=temp` 上传到 ComfyUI 的 `temp/tryon/` 目录，文件名带随机 ID，不覆盖他人文件；ComfyUI **重启时会清空 temp 目录**。
- 任务结束（成功、失败或客户端断开/超时）后，后端会删除该任务的 `/history` 记录；超时或断开时还会从队列删除任务，若正在执行则只中断该 `prompt_id`。
- 工作流输出节点若为 `SaveImage`，结果图会写入 ComfyUI 的 `output/tryon/` 目录并**长期保留**。建议二选一：
  1. 把输出节点改为 `PreviewImage`（写入 temp 目录，后端会自动跳过 `filename_prefix`）；
  2. 在 ComfyUI 主机上定时清理，例如 `find /path/to/ComfyUI/output/tryon /path/to/ComfyUI/temp/tryon -type f -mmin +60 -delete`。
- ComfyUI 本身没有鉴权，请只监听本机或内网地址，不要直接暴露到公网。

### 9. 持续安全检查（建议接入 CI）
```bash
cd backend
go vet ./... && go test -race ./...
go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...
# 密钥扫描（仓库根目录）
gitleaks detect --source . --redact
node ../tests/frontend_tryon_test.mjs
```

### 10. 模型内容安全遵从
各模型提供商（如 OpenAI、Google、火山引擎）对真人图像生成均有内容安全审核（Moderation），请遵守相关法律法规与平台服务条款，勿用于违背公序良俗的场景。

## 五点五、探针与优雅停机

- `GET /healthz`：存活探针，进程存活即返回 `200 {"status":"ok"}`；镜像 `HEALTHCHECK` 与 compose 健康检查使用它。
- `GET /readyz`：就绪探针，以下条件全部满足才返回 200，否则 503：配置已加载、至少一个**云端模型**已配置 Key（ComfyUI 不计入，其可用性见 `/comfyui/health`）、状态存储可用、且未进入停机流程。响应体只包含每项的 `ok` / `fail` / `draining`，不含错误细节。
- 两个探针都不鉴权、不限流、不调用任何付费上游；公网部署时请在 Nginx 中只允许内网或监控节点访问（`/hello` 保留兼容旧的探测配置）。
- 收到 `SIGTERM` / `SIGINT` 后：`/readyz` 立即返回 503 → 等待 `shutdown.drainDelaySeconds`（默认 3 秒，期间仍处理新请求）→ 停止接收新连接并等待在途请求完成，最多 `shutdown.timeoutSeconds`（默认 190 秒）→ 关闭状态存储并退出。
- 不再使用 GoFrame 自带的信号处理：实测其收到 SIGTERM 后会立即中断在途的换装请求（`gracefulTimeout` 默认仅 2 秒）。
- `docker-compose.yml` 已设置 `stop_grace_period: 200s`；`docker stop` / `docker run` 请相应使用 `-t 200`（`--stop-timeout 200`），否则 Docker 默认 10 秒后强杀。`shutdown.timeoutSeconds` 必须大于模型请求超时。

## 六、验证部署

部署后测试（若开启了鉴权，请带上 `Authorization: Bearer <token>`）：

```bash
# 未带 Token 或错误 Token（预期返回 HTTP 401: AUTH_REQUIRED）
curl -X POST https://api.你的域名/tryon -H "Content-Type: application/json" -d '{}'

# 预期返回：{"code":40101,"message":"请在设置中心填写后端访问 Token","data":{"error_code":"AUTH_REQUIRED","request_id":"..."}}

# 携带合法 Token 验证连通性
curl -X POST https://api.你的域名/tryon \
  -H "Authorization: Bearer 你的Token" \
  -H "Content-Type: application/json" \
  -d '{}'

# 预期返回 HTTP 400 参数验证错误：{"code":51,"message":"The PersonImage field is required","data":{"request_id":"..."}}

# 就绪探针（在服务器本机执行；未配置任何云端模型 Key 时返回 503）
curl -s http://127.0.0.1:8000/readyz
```

看到这个响应，说明后端已正常运行，即可在前端使用「AI 换装」。
