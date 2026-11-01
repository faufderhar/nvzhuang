# 首次装机运行手册（交付 AI 逐步执行版）

本手册面向**一台全新的海外 VPS**，从空系统一路执行到「服务可用并通过验收」。执行者（人或 AI）拥有该 VPS 的 shell，按章节**从上到下**执行，每一步都给出了命令与预期输出；任何一步与预期不符，先查 [§12 故障排查](#12-故障排查)，仍无法解决就**停止并向用户报告**，不要跳过、不要自行放宽安全设置。

本手册与仓库现状严格一致（`docker-compose.yml`、`deploy/docker-compose.prod.yml`、`deploy/nginx/templates/nvzhuang.conf.template`、`deploy/host/init-host.sh`、`backend/manifest/config/config.example.yaml`），背景说明见 [`deploy/README.md`](README.md) 与 [`backend/DEPLOY.md`](../backend/DEPLOY.md)。验收由 [`deploy/verify.sh`](verify.sh) 自动完成。

拓扑（与仓库一致，**不使用宿主机 Nginx**）：

```
浏览器（前端静态站：GitHub Pages 等）
   │ HTTPS
   ▼
Nginx 容器 costume-tryon-nginx 172.30.0.2（唯一发布 80/443 的服务）
   │ HTTP（内部网络 172.30.0.0/24）
   ▼
后端容器 costume-tryon-backend 172.30.0.10:8000（不发布任何端口）
   │ HTTPS
   ▼
云端模型（Gemini / OpenAI 兼容 / 豆包）
```

> 前端不部署在本 VPS 上：仓库约定前端是静态站（默认 GitHub Pages），本 VPS 只提供 API。用户在前端「设置中心」填写 `https://<API_DOMAIN>` 与个人 Token 即可使用。

## 执行约定（AI 必读）

1. **缺任何必填输入就停止并询问用户**（见 §1），不要猜测、不要使用示例值。
2. **不得在对话、日志或命令行参数中回显任何密钥或 Token**。模型 Key 由用户自己写入服务器文件（§5.1）；Token 由服务器生成并只写入 0600 文件。查看是否配置成功一律用「set/empty」式检查，不打印值。
3. 不要提交、推送任何服务器上的配置文件；仓库里只有占位值。
4. 所有命令都在 bash 中执行。从 §3.3 开始，每个新 shell 先执行：
   ```bash
   source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang 2>/dev/null || true
   ```
5. SSH 相关改动（§3、§6）期间**始终保留一个已登录的会话**，另开新会话验证能登录后才关闭旧会话。
6. 时间：服务器统一使用 `HOST_TIMEZONE`（默认 UTC）；后端每日额度按 UTC 自然日重置。

---

## 1. 输入清单

开始前向用户逐项确认。**标注「必填」的任何一项缺失：停止并询问用户，不要继续。**

| 变量 | 必填 | 含义 | 占位示例（不可直接使用） |
|---|---|---|---|
| `SERVER_PUBLIC_IP` | 必填 | VPS 公网 IPv4 | `203.0.113.10` |
| `API_DOMAIN` | 必填 | 对外 API 域名，A 记录指向 `SERVER_PUBLIC_IP`，**不要配 AAAA**（见 §2.4） | `api.example.com` |
| `FRONTEND_ORIGIN` | 必填 | 前端来源（协议+主机，无路径、无结尾斜杠），写入 CORS 白名单 | `https://frontend.example.com`（GitHub Pages 为 `https://<用户名>.github.io`） |
| `FRONTEND_URL` | 可选 | 前端页面地址，验收时检查可访问；默认 `${FRONTEND_ORIGIN}/` | `https://<用户名>.github.io/nvzhuang/` |
| `ADMIN_USER` | 必填 | 新建的非 root 管理用户名（`^[a-z_][a-z0-9_-]{0,31}$`） | `deploy` |
| `ADMIN_SSH_PUBKEY` | 必填 | 该用户登录用的 SSH **公钥**（一行，`ssh-ed25519 ...`） | `ssh-ed25519 AAAA... ops` |
| `SSH_ALLOW_CIDR` | 必填 | 允许 SSH 的来源 IP/网段（不能是 `0.0.0.0/0`）；**必须包含执行者当前的出口地址** | `198.51.100.7/32` |
| `SSH_PORT` | 可选 | SSH 端口，默认 `22`；改端口须同时放行云厂商安全组 | `22` |
| `HOST_TIMEZONE` | 可选 | 默认 `UTC` | `UTC` |
| `REPO_URL` | 必填 | 仓库 Git 地址（HTTPS） | `https://github.com/<owner>/nvzhuang.git` |
| `DEPLOY_REF` | 必填 | 要部署的 **tag 或完整 commit SHA**（不要用分支名）；必须包含 `deploy/FIRST-INSTALL.md` 与 `deploy/verify.sh`；应使用包含 2026-11-01 安全修复（`fix/security-audit` 合并后）的 tag/SHA | `v1.2.0` 或 40 位 SHA |
| `CERT_MODE` | 必填 | `letsencrypt`（自动签发，推荐）或 `existing`（用户提供证书文件） | `letsencrypt` |
| `CERT_EMAIL` | `letsencrypt` 时必填 | Let's Encrypt 到期通知邮箱 | `ops@example.com` |
| `MODELS` | 必填 | 启用的模型名，空格分隔，取值 `gemini` `gpt-image` `doubao`（与前端选项同名），至少一个 | `gemini gpt-image` |
| 模型 Key | 必填 | 每个启用模型对应的 Key：`gemini`→`GEMINI_API_KEY`，`gpt-image`→`OPENAI_API_KEY`，`doubao`→`ARK_API_KEY`。**由用户本人**按 §5.1 写入服务器，不经过 AI 对话 | — |
| 模型 baseURL / model | 可选 | 默认取 `config.example.yaml` 中的值（OpenAI 兼容中转站等需改 `gpt-image` 的 `baseURL`，必须是公网 HTTPS） | `https://api.openai.com/v1` |
| `TOKEN_NAMES` | 必填 | 使用者名单（每人一个后端访问 Token，只用作文件内备注，可用代号），空格分隔 | `alice bob` |
| `ENABLE_SWAP` | 可选 | `1` 时创建 2G swap，默认 `1` | `1` |
| `VERIFY_PERSON_IMAGE` | 可选 | 真实换装验收用的人像照片路径（需本人同意；会消耗一次模型调用费用）。不提供则跳过真实生图 | `/home/deploy/verify-person.jpg` |
| `BACKUP_PULL_HOST` | 可选 | 负责拉取备份的机器（备份节点）公网 IP，用于放行其 SSH | `198.51.100.20` |
| `STANDBY_IP` | 可选 | 4c8g 热备节点公网 IP（附录 A） | `203.0.113.20` |
| `ALERT_CONTACT` | 可选 | 外部可用性监控告警接收人/渠道（§11.6） | — |

---

## 2. 预检

以 root（或可 sudo 的初始用户）登录 VPS。

### 2.1 写入输入文件

把 §1 的**非机密**输入写入 `/etc/nvzhuang/inputs.env`（不含任何 Key/Token），后续每一步都会 `source` 它：

```bash
sudo install -d -m 0755 /etc/nvzhuang
sudo tee /etc/nvzhuang/inputs.env >/dev/null <<'EOF2'
SERVER_PUBLIC_IP=203.0.113.10
API_DOMAIN=api.example.com
FRONTEND_ORIGIN=https://frontend.example.com
FRONTEND_URL=https://frontend.example.com/
ADMIN_USER=deploy
SSH_ALLOW_CIDR=198.51.100.7/32
SSH_PORT=22
HOST_TIMEZONE=UTC
REPO_URL=https://github.com/OWNER/nvzhuang.git
DEPLOY_REF=REPLACE_WITH_TAG_OR_SHA
CERT_MODE=letsencrypt
CERT_EMAIL=ops@example.com
MODELS="gemini"
TOKEN_NAMES="alice bob"
ENABLE_SWAP=1
# 统一的 compose 调用：固定项目名，两个 --env-file 缺一不可（原因见 §5.3）
dc() { sudo docker compose -p nvzhuang --env-file .env --env-file deploy/.env.prod -f docker-compose.yml -f deploy/docker-compose.prod.yml "$@"; }
EOF2
sudo chmod 0644 /etc/nvzhuang/inputs.env
```

> 上面全部是占位值，**必须**替换成用户提供的真实值。替换后自检（应无输出）：
> ```bash
> grep -nE 'example\.(com|org)|203\.0\.113\.|198\.51\.100\.|OWNER|REPLACE_WITH' /etc/nvzhuang/inputs.env
> ```

### 2.2 系统与资源

```bash
source /etc/nvzhuang/inputs.env
. /etc/os-release; echo "$ID $VERSION_ID"; uname -m
nproc; free -g | awk '/^Mem:/{print $2" GiB"}'; df -BG --output=avail / | tail -1
```

预期：
- `ubuntu 24.04`（推荐；`ubuntu 22.04`、`debian 12` 也可，`init-host.sh` 只支持 apt 系发行版）；架构 `x86_64` 或 `aarch64`。
- CPU ≥ 4；内存 ≥ 8 GiB（主节点推荐 24 GiB；后端容器上限 3 GiB + Nginx 256 MiB + 构建期峰值）；根分区可用 ≥ 20G。
- 不满足：停止并报告。

### 2.3 端口

```bash
sudo ss -ltnp '( sport = :80 or sport = :443 )'
```

预期：只有表头、没有监听进程。若有 `nginx`/`apache2`/`caddy` 占用：本部署**不使用宿主机 Web 服务器**，与用户确认后执行 `sudo systemctl disable --now nginx apache2 caddy 2>/dev/null`，再复查为空。

云厂商安全组/防火墙（控制台操作，AI 无法代做时请用户完成并确认）：入站只放行 TCP 80、443，以及来自 `SSH_ALLOW_CIDR` 的 `SSH_PORT`。

### 2.4 DNS

```bash
source /etc/nvzhuang/inputs.env
getent ahostsv4 "$API_DOMAIN" | awk '{print $1}' | sort -u
getent ahostsv6 "$API_DOMAIN" | grep -v '::ffff:' | awk '{print $1}' | sort -u
```

预期：第一条**只输出** `SERVER_PUBLIC_IP`；第二条**无输出**。

- A 记录不对：请用户修正 DNS 后等待生效再继续（Let's Encrypt HTTP-01 依赖它）。
- 存在 AAAA：请用户删除。原因：生产 compose 只在 IPv4 上发布 80/443（Nginx 容器没有 IPv6 地址，若发布 IPv6，连接会被 Docker 的用户态代理转成网关地址 `172.30.0.1`，导致所有 IPv6 用户共用同一个限流/额度桶，且能访问只对本机开放的 `/healthz`、`/readyz`），有 AAAA 时 IPv6 客户端会先连接失败再回退 IPv4。

### 2.5 出站连通性

```bash
for u in https://download.docker.com https://registry-1.docker.io/v2/ https://github.com \
         https://generativelanguage.googleapis.com https://api.openai.com https://ark.cn-beijing.volces.com; do
  printf '%-45s %s\n' "$u" "$(curl -sS -o /dev/null -m 10 -w '%{http_code}' "$u" 2>&1)"
done
```

预期：每行都是三位 HTTP 状态码（200/301/401/404 等均可），**不能是 `000` 或报错**。未启用的模型对应的行可以忽略。

---

## 3. 基础系统

### 3.1 系统更新

```bash
sudo env DEBIAN_FRONTEND=noninteractive apt-get update -q
sudo env DEBIAN_FRONTEND=noninteractive apt-get -y -q upgrade
sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -q git openssl ca-certificates curl gnupg
[ -f /var/run/reboot-required ] && echo REBOOT_REQUIRED || echo NO_REBOOT
```

若输出 `REBOOT_REQUIRED`：`sudo reboot`，重新登录后继续。

### 3.2 非 root 管理用户

```bash
source /etc/nvzhuang/inputs.env
ADMIN_SSH_PUBKEY='ssh-ed25519 AAAA...替换为用户提供的公钥'
id "$ADMIN_USER" >/dev/null 2>&1 || sudo adduser --disabled-password --gecos "" "$ADMIN_USER"
sudo usermod -aG sudo "$ADMIN_USER"
sudo install -d -m 0700 -o "$ADMIN_USER" -g "$ADMIN_USER" "/home/$ADMIN_USER/.ssh"
printf '%s\n' "$ADMIN_SSH_PUBKEY" | sudo tee -a "/home/$ADMIN_USER/.ssh/authorized_keys" >/dev/null
sudo chown "$ADMIN_USER:$ADMIN_USER" "/home/$ADMIN_USER/.ssh/authorized_keys"
sudo chmod 0600 "/home/$ADMIN_USER/.ssh/authorized_keys"
# 该用户没有密码，给免密 sudo（便于自动化执行；交付后如需收紧可删除此文件并为用户设置密码）
echo "$ADMIN_USER ALL=(ALL) NOPASSWD:ALL" | sudo tee /etc/sudoers.d/90-nvzhuang-admin >/dev/null
sudo chmod 0440 /etc/sudoers.d/90-nvzhuang-admin && sudo visudo -cf /etc/sudoers.d/90-nvzhuang-admin
```

预期最后一行：`/etc/sudoers.d/90-nvzhuang-admin: parsed OK`。

**另开一个新的 SSH 会话**以 `ADMIN_USER` 登录，执行 `sudo -n true && echo SUDO_OK`，预期 `SUDO_OK`。成功后**后续全部步骤都用 `ADMIN_USER` 执行**（§6 会禁止 root 登录）。

### 3.3 SSH 加固、防火墙、时间同步

由仓库脚本 `deploy/host/init-host.sh` 统一完成（§6）：仅公钥登录、禁止 root 登录、`AllowUsers ADMIN_USER`、ufw 默认拒绝入站且只放行 80/443 与来自 `SSH_ALLOW_CIDR` 的 SSH、fail2ban、unattended-upgrades、chrony 时间同步与时区、Docker 日志上限。这里不手工改，避免与脚本冲突。

### 3.4 Swap（可选，`ENABLE_SWAP=1`）

```bash
source /etc/nvzhuang/inputs.env
if [ "${ENABLE_SWAP:-1}" = 1 ] && ! swapon --show=NAME --noheadings | grep -q .; then
  sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
  grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab >/dev/null
  echo 'vm.swappiness=10' | sudo tee /etc/sysctl.d/90-nvzhuang-swap.conf >/dev/null && sudo sysctl -q --system
fi
swapon --show
```

预期：已有 swap 或显示 `/swapfile ... 2G`。（容器设置了 `memswap_limit: 3g`，后端不会使用 swap；swap 只给宿主机兜底。）

### 3.5 Docker Engine 与 Compose 插件

使用 Docker 官方 apt 源（Debian 把下面两处 `ubuntu` 换成 `debian`）：

```bash
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}") stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null
sudo env DEBIAN_FRONTEND=noninteractive apt-get update -q
sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -q docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker containerd
sudo docker version --format '{{.Server.Version}}'
sudo docker compose version --short
systemctl is-enabled docker
```

预期：输出 Docker 版本；Compose 版本 **≥ 2.24.0**（`deploy/docker-compose.prod.yml` 使用 `!reset`，低版本会报错）；最后一行 `enabled`。

> 不把 `ADMIN_USER` 加入 `docker` 组（等同 root），本手册统一用 `sudo docker`。

### 3.6 Nginx

**不在宿主机安装 Nginx。** 仓库的 Nginx 以容器 `costume-tryon-nginx`（镜像 `nginx:1.28-alpine`）运行，由 compose 管理，配置来自 `deploy/nginx/templates/nvzhuang.conf.template`。§2.3 已确认 80/443 空闲。

---

## 4. 拉取代码（固定版本）

```bash
source /etc/nvzhuang/inputs.env
sudo install -d -m 0755 -o "$ADMIN_USER" -g "$ADMIN_USER" /opt/nvzhuang
git clone "$REPO_URL" /opt/nvzhuang
cd /opt/nvzhuang
git -c advice.detachedHead=false checkout --detach "$DEPLOY_REF"
git rev-parse HEAD
git status --porcelain | wc -l
ls deploy/FIRST-INSTALL.md deploy/verify.sh deploy/host/init-host.sh deploy/docker-compose.prod.yml
```

预期：打印 40 位 SHA（记录下来，写入交付报告）；`git status` 计数为 `0`；四个文件都存在（不存在说明 `DEPLOY_REF` 太旧：停止并询问用户）。

---

## 5. 配置

### 5.1 模型 Key（由用户本人写入）

请用户自己 SSH 登录后执行下面的命令（`sudoedit` 会打开编辑器），每行 `变量名=值`，只写启用模型对应的变量：

```bash
sudo install -m 0600 -o root -g root /dev/null /etc/nvzhuang/model-keys.env   # 只在文件不存在时执行
sudoedit /etc/nvzhuang/model-keys.env
# 内容示例（值替换为真实 Key）：
# GEMINI_API_KEY=...
# OPENAI_API_KEY=...
# ARK_API_KEY=...
```

AI 只做**不打印值**的校验：

```bash
source /etc/nvzhuang/inputs.env
sudo stat -c '%a %U' /etc/nvzhuang/model-keys.env
sudo grep -cvE '^(GEMINI_API_KEY|OPENAI_API_KEY|ARK_API_KEY|DASHSCOPE_API_KEY)=[^[:space:]]+$' /etc/nvzhuang/model-keys.env
for m in $MODELS; do
  case "$m" in gemini) k=GEMINI_API_KEY ;; gpt-image) k=OPENAI_API_KEY ;; doubao) k=ARK_API_KEY ;; *) echo "未知模型 $m"; continue ;; esac
  sudo grep -qE "^$k=.+" /etc/nvzhuang/model-keys.env && echo "$m: $k=set" || echo "$m: $k=MISSING"
done
```

预期：`600 root`；第二行 `0`（没有格式不符的行；空行也算不符，删掉即可）；每个模型都是 `=set`。有 `MISSING`：停止并请用户补充。

### 5.2 后端访问 Token

每个使用者一个 32 字节随机 Token，另加一个只用于验收的 `verify` Token：

```bash
source /etc/nvzhuang/inputs.env
sudo test -e /etc/nvzhuang/tokens.tsv || sudo sh -c "umask 077; for n in verify $TOKEN_NAMES; do printf '%s\t%s\n' \"\$n\" \"\$(openssl rand -hex 32)\"; done > /etc/nvzhuang/tokens.tsv"
sudo sh -c "umask 077; awk -F'\t' '\$1==\"verify\"{print \$2}' /etc/nvzhuang/tokens.tsv > /etc/nvzhuang/verify.token"
sudo stat -c '%a %U %n' /etc/nvzhuang/tokens.tsv /etc/nvzhuang/verify.token
sudo awk -F'\t' '{print $1, length($2)}' /etc/nvzhuang/tokens.tsv
```

预期：两个文件都是 `600 root`；每个名字后面是 `64`。**不要 cat 这两个文件**。交付时告诉用户：用自己的 SSH 会话执行 `sudo cat /etc/nvzhuang/tokens.tsv` 取回各人 Token，经私密渠道分发。

### 5.3 根目录 `.env`（机密，0600）

```bash
cd /opt/nvzhuang
sudo sh -c 'umask 077; { cat /etc/nvzhuang/model-keys.env; printf "TRYON_API_KEYS=%s\n" "$(cut -f2 /etc/nvzhuang/tokens.tsv | paste -sd, -)"; } > /opt/nvzhuang/.env'
sudo chown root:root /opt/nvzhuang/.env && sudo chmod 0600 /opt/nvzhuang/.env
sudo stat -c '%a %U' /opt/nvzhuang/.env
sudo sed -E 's/=.*/=***/' /opt/nvzhuang/.env
test ! -e backend/.env && echo NO_BACKEND_ENV
git check-ignore -q .env && echo IGNORED
```

预期：`600 root`；变量名列表（值已打码）包含启用的模型 Key 与 `TRYON_API_KEYS`；`NO_BACKEND_ENV`；`IGNORED`。

> **为什么 Key 必须放根目录 `.env`，且必须用 `dc` 里的两个 `--env-file`**：根 `docker-compose.yml` 的 `environment:` 写了 `OPENAI_API_KEY=${OPENAI_API_KEY:-}` 等四个变量，它们由 compose **插值**得到，且优先级高于 `env_file`。只写 `--env-file deploy/.env.prod` 时 compose 不再读取根目录 `.env` 做插值，这四个变量会被插值成空串并**覆盖** `env_file` 里的真实 Key，`/readyz` 的 `models` 会失败。`dc` 先读 `.env` 再读 `deploy/.env.prod` 即可避免；`backend/.env` 不参与插值，生产上不要使用。

### 5.4 `deploy/.env.prod`（非机密）

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
mkdir -p deploy/nginx/acme
if [ "$CERT_MODE" = letsencrypt ]; then TLS_DIR=/etc/letsencrypt; CERT=/etc/letsencrypt/live/$API_DOMAIN/fullchain.pem; KEY=/etc/letsencrypt/live/$API_DOMAIN/privkey.pem
else TLS_DIR=/etc/nvzhuang/tls; CERT=$TLS_DIR/fullchain.pem; KEY=$TLS_DIR/privkey.pem; fi
cat > deploy/.env.prod <<EOF2
API_SERVER_NAME=$API_DOMAIN
TLS_DIR=$TLS_DIR
TLS_CERT_PATH=$CERT
TLS_KEY_PATH=$KEY
ACME_WEBROOT=/opt/nvzhuang/deploy/nginx/acme
NVZHUANG_CONFIG_PATH=./backend/manifest/config/config.yaml
EOF2
chmod 0644 deploy/.env.prod
cat deploy/.env.prod
git check-ignore -q deploy/.env.prod && echo IGNORED
```

预期：六行内容已替换为真实域名与路径；`IGNORED`。

### 5.5 后端 `config.yaml`

生成生产配置（键与取值来自 `config.example.yaml` + `deploy/backend/config.prod.overrides.yaml`，只包含 `MODELS` 中启用的模型；`/readyz` 要求至少一个已配置 Key 的模型）：

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
for m in $MODELS; do case "$m" in gemini|gpt-image|doubao) ;; *) echo "STOP: MODELS 含未知模型 $m";; esac; done
CFG=backend/manifest/config/config.yaml
model_block() {
  case "$1" in
    gemini) cat <<'Y'
    gemini:
      provider: gemini
      apiKey: ""
      envKey: "GEMINI_API_KEY"
      baseURL: "https://generativelanguage.googleapis.com/v1beta"
      model: "gemini-3.1-flash-image"
Y
    ;;
    gpt-image) cat <<'Y'
    gpt-image:
      provider: openai
      apiKey: ""
      envKey: "OPENAI_API_KEY"
      baseURL: "https://api.openai.com/v1"
      model: "gpt-image-2"
Y
    ;;
    doubao) cat <<'Y'
    doubao:
      provider: doubao
      apiKey: ""
      envKey: "ARK_API_KEY"
      baseURL: "https://ark.cn-beijing.volces.com/api/v3"
      model: "doubao-seedream-3-0-t2i-250415"
Y
    ;;
    *) echo "未知模型: $1" >&2; return 1 ;;
  esac
}
{
cat <<'Y'
# 生产配置（由 deploy/FIRST-INSTALL.md 生成；不得提交）。模型 Key 只通过 envKey 环境变量注入，apiKey 保持为空。
server:
  address: ":8000"
shutdown:
  drainDelaySeconds: 3
  timeoutSeconds: 190
logger:
  level: "info"
  stdout: true
llm:
  requestTimeoutSeconds: 180
  models:
Y
for m in $MODELS; do model_block "$m"; done
cat <<Y
security:
  authRequired: true
  apiKeysEnv: "TRYON_API_KEYS"
  trustedOrigins:
    - "$FRONTEND_ORIGIN"
  allowPrivateNetwork: false
  trustedProxies: ["172.30.0.2/32"]
  clientModelAllowedPorts: [443]
  stateDir: "/app/data"
  rateLimit:
    requestsPerMinute: 10
    burst: 3
    limiterIdleMinutes: 30
    dailyRequestsPerPrincipal: 100
  concurrency:
    global: 6
    perPrincipal: 1
    queueTimeoutSeconds: 10
upload:
  maxRequestBodyBytes: 25165824
  maxEncodedImageBytes: 12582912
  maxDecodedImageBytes: 8388608
  maxCombinedDecodedBytes: 16777216
  minWidth: 256
  minHeight: 256
  maxWidth: 8192
  maxHeight: 8192
  maxPixels: 20000000
  allowedMimeTypes:
    - "image/jpeg"
    - "image/png"
    - "image/webp"
Y
} > /tmp/nvz-config.yaml && sudo install -m 0640 -o root -g 10001 /tmp/nvz-config.yaml "$CFG" && rm -f /tmp/nvz-config.yaml
sudo stat -c '%a %U %g' "$CFG"
git check-ignore -q "$CFG" && echo IGNORED
grep -cE 'sk-|AIza|apiKey: "[^"]' "$CFG" || true
```

预期：第一段没有 `STOP:` 输出（有则停止并与用户确认模型名）；`640 root 10001`（容器内后端以 UID/GID 10001 运行，需要组读权限；`root:root 0640` 会导致容器读不到配置）；`IGNORED`；最后一行 `0`（配置里没有明文 Key）。

如用户要求自定义 `baseURL`/`model`（如 OpenAI 兼容中转），用 `sudoedit` 修改对应模型块；`baseURL` 必须是公网 `https://`。

### 5.6 compose 静态校验

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
dc config --quiet && echo CONFIG_OK
dc config --images
dc config | grep -nE 'published|target: (80|443)|ipv4_address|GOMEMLIMIT'
dc config --format json | python3 -c 'import json,sys;e=json.load(sys.stdin)["services"]["costume-tryon"]["environment"];print({k:("set" if v else "empty") for k,v in e.items() if k.endswith("_KEY") or k=="TRYON_API_KEYS"})'
```

预期：
- `CONFIG_OK`；镜像为 `nvzhuang-costume-tryon` 与 `nginx:1.28-alpine`。
- `published` 只出现 `"80"` 和 `"443"`（都属于 nginx 服务），`ipv4_address` 为 `172.30.0.10` 与 `172.30.0.2`，有 `GOMEMLIMIT: 2GiB`。
- 最后一行中启用模型对应的 Key 为 `set`（未启用的为 `empty` 正常）。若为 `empty`：检查 §5.3（Key 不在根 `.env`，或没用 `dc`）。

---

## 6. 主机初始化：`init-host.sh`

### 6.1 预览（dry-run，不改系统）

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
echo "当前 SSH 来源: ${SSH_CONNECTION%% *}  允许网段: $SSH_ALLOW_CIDR"
sudo ADMIN_USER="$ADMIN_USER" SSH_ALLOW_CIDR="$SSH_ALLOW_CIDR" SSH_PORT="${SSH_PORT:-22}" HOST_TIMEZONE="${HOST_TIMEZONE:-UTC}" \
  bash deploy/host/init-host.sh
```

预期：一系列 `==>` 与 `[dry-run] ...` 行，最后一行 `==> 以上为预览，未做任何修改。确认无误后加 --apply 执行`。逐项核对：

- 写入 `/etc/ssh/sshd_config.d/10-nvzhuang-hardening.conf`，含 `Port <SSH_PORT>`、`PasswordAuthentication no`、`PermitRootLogin no`、`AllowUsers <ADMIN_USER>`；
- `ufw allow from <SSH_ALLOW_CIDR> to any port <SSH_PORT> proto tcp`、`ufw allow 80/tcp`、`ufw allow 443/tcp`；
- **当前 SSH 来源地址必须落在 `SSH_ALLOW_CIDR` 内**，否则启用防火墙后会失联：停止并请用户修正 `SSH_ALLOW_CIDR`。

### 6.2 执行

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
sudo ADMIN_USER="$ADMIN_USER" SSH_ALLOW_CIDR="$SSH_ALLOW_CIDR" SSH_PORT="${SSH_PORT:-22}" HOST_TIMEZONE="${HOST_TIMEZONE:-UTC}" \
  bash deploy/host/init-host.sh --apply
```

预期最后一行：`==> 完成。请保持当前 SSH 会话，另开终端验证能用公钥登录端口 <SSH_PORT> 后再退出`。脚本若因 `没有 ~/.ssh/authorized_keys` 中止，回到 §3.2。

Docker 已在 §3.5 安装，脚本新写入的 `/etc/docker/daemon.json` 需要重启 Docker 才生效（此时还没有业务容器，可直接重启）：

```bash
sudo systemctl restart docker
sudo docker info --format '{{.LoggingDriver}} {{.LiveRestoreEnabled}}'
```

预期：`json-file true`（若脚本提示 daemon.json 已存在未覆盖，需人工确认其中有 `max-size`/`max-file`）。

### 6.3 验证（不关闭当前会话）

**另开新会话**用 `ADMIN_USER` + 公钥登录（端口为 `SSH_PORT`）成功后，在新会话执行：

```bash
sudo sshd -T | grep -E '^(port|passwordauthentication|permitrootlogin|allowusers) '
sudo ufw status verbose
timedatectl show -p Timezone -p NTPSynchronized
systemctl is-active fail2ban chrony unattended-upgrades
```

预期：
- `port <SSH_PORT>`、`passwordauthentication no`、`permitrootlogin no`、`allowusers <ADMIN_USER>`；
- `Status: active`，`Default: deny (incoming), allow (outgoing)`，规则只有：`<SSH_PORT>/tcp ALLOW IN <SSH_ALLOW_CIDR>`、`80/tcp ALLOW IN Anywhere`、`443/tcp ALLOW IN Anywhere`（以及对应的 `(v6)` 行）；
- `Timezone=<HOST_TIMEZONE>`，`NTPSynchronized=yes`（刚启动 chrony 时可能需要 1–2 分钟）；
- 三个 `active`。

新会话登录失败：**不要关闭旧会话**，在旧会话中执行 `sudo rm /etc/ssh/sshd_config.d/10-nvzhuang-hardening.conf && sudo systemctl restart ssh`（必要时 `sudo ufw disable`）恢复后排查，再重跑 §6.2。

如配置了备份节点 `BACKUP_PULL_HOST`，额外放行其 SSH：`sudo ufw allow from "$BACKUP_PULL_HOST" to any port "${SSH_PORT:-22}" proto tcp`（备份节点同样以 `ADMIN_USER` + 它自己的公钥登录）。

> 注意：Docker 发布的端口会绕过 ufw。生产 compose 只发布 Nginx 的 80/443，后端端口已用 `ports: !reset []` 清除——**不要给后端加任何端口映射**。

---

## 7. TLS 证书

Nginx 模板启动时就需要证书文件，因此先签发证书、再启动服务。

### 7.1 `CERT_MODE=letsencrypt`

此时 80 端口空闲（服务尚未启动），用 standalone 模式首次签发：

```bash
source /etc/nvzhuang/inputs.env
sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -q certbot
certbot --version
sudo certbot certonly --standalone -d "$API_DOMAIN" -m "$CERT_EMAIL" --agree-tos --no-eff-email -n
sudo openssl x509 -in "/etc/letsencrypt/live/$API_DOMAIN/fullchain.pem" -noout -checkhost "$API_DOMAIN" -enddate
```

预期：`certbot 2.x`（**≥ 2.3**，§9.2 的 `certbot reconfigure` 需要；Ubuntu 24.04 apt 版本满足，更旧的系统用 `sudo snap install --classic certbot`）；`Successfully received certificate.`；`Hostname <API_DOMAIN> does match certificate` 与 `notAfter=...`（约 90 天后）。

失败多为 DNS 未生效或 80 端口被云安全组拦截（见 §12）。续期方式在服务启动后切换为 webroot（§9.2）。

### 7.2 `CERT_MODE=existing`

请用户把证书链与私钥放到服务器（AI 不经手私钥内容）：

```bash
sudo install -d -m 0755 /etc/nvzhuang/tls
# 用户自行放置：/etc/nvzhuang/tls/fullchain.pem（0644）与 /etc/nvzhuang/tls/privkey.pem（0600，root）
source /etc/nvzhuang/inputs.env
sudo openssl x509 -in /etc/nvzhuang/tls/fullchain.pem -noout -checkhost "$API_DOMAIN" -enddate
[ "$(sudo openssl x509 -in /etc/nvzhuang/tls/fullchain.pem -noout -pubkey | sha256sum)" = "$(sudo openssl pkey -in /etc/nvzhuang/tls/privkey.pem -pubout | sha256sum)" ] && echo KEY_MATCH
sudo stat -c '%a %U %n' /etc/nvzhuang/tls/privkey.pem
```

预期：`... does match certificate`、到期时间在未来、`KEY_MATCH`、`600 root`。该模式下续期由用户负责：替换文件后执行 `sudo docker exec costume-tryon-nginx nginx -s reload`。

---

## 8. 构建与启动

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
dc build --pull
dc up -d
for i in $(seq 1 36); do
  s="$(sudo docker inspect -f '{{.State.Health.Status}}' costume-tryon-backend costume-tryon-nginx 2>/dev/null | paste -sd' ')"
  echo "[$i] $s"; [ "$s" = "healthy healthy" ] && break; sleep 5
done
dc ps
sudo docker logs costume-tryon-backend 2>&1 | tail -n 20
```

预期：
- 构建成功（后端镜像 `nvzhuang-costume-tryon`，基于 `golang:1.26-alpine` 构建、`alpine:3.22` 运行）；
- 3 分钟内打印 `healthy healthy`；
- `dc ps` 中 `costume-tryon-backend` 的 PORTS 为空，`costume-tryon-nginx` 为 `0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp`（显示格式可能略有不同，但只能有 80/443，且**不能**出现 `[::]`）；
- 后端日志没有 `【部署告警】`、没有 `环境变量 TRYON_API_KEYS 中包含示例/占位 Token`、没有 `permission denied`。

后端反复重启或 `up -d` 卡在等待后端健康：看 `sudo docker logs costume-tryon-backend`，按 §12 处理。

---

## 9. Nginx 配置与重载

### 9.1 校验渲染结果

官方镜像在**容器启动时**把 `deploy/nginx/templates/nvzhuang.conf.template` 渲染到容器内 `/etc/nginx/conf.d/nvzhuang.conf`（只替换 `API_SERVER_NAME`、`TLS_CERT_PATH`、`TLS_KEY_PATH`）。

```bash
source /etc/nvzhuang/inputs.env
sudo docker exec costume-tryon-nginx nginx -t
sudo docker exec costume-tryon-nginx grep -c "server_name $API_DOMAIN;" /etc/nginx/conf.d/nvzhuang.conf
sudo docker exec costume-tryon-nginx grep -c '\${' /etc/nginx/conf.d/nvzhuang.conf
sudo docker exec costume-tryon-nginx nginx -s reload && echo RELOAD_OK
```

预期：`syntax is ok` 与 `test is successful`；`2`；`0`（没有未替换的占位符）；`RELOAD_OK`。

- 只改了证书文件 → `nginx -s reload` 即可。
- 改了模板或 `deploy/.env.prod` → 必须重建容器重新渲染：`dc up -d --force-recreate nginx`，然后重做本节。

### 9.2 证书续期切换为 webroot（仅 `CERT_MODE=letsencrypt`）

```bash
source /etc/nvzhuang/inputs.env
W=/opt/nvzhuang/deploy/nginx/acme
sudo install -d -m 0755 "$W/.well-known/acme-challenge"
echo nvz-acme-ok | sudo tee "$W/.well-known/acme-challenge/nvz-ping" >/dev/null
curl -s --resolve "$API_DOMAIN:80:127.0.0.1" "http://$API_DOMAIN/.well-known/acme-challenge/nvz-ping"; sudo rm -f "$W/.well-known/acme-challenge/nvz-ping"
sudo certbot reconfigure --cert-name "$API_DOMAIN" --webroot -w "$W" -n
sudo install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
printf '#!/bin/sh\n# 证书续期后让 Nginx 容器加载新证书\ndocker exec costume-tryon-nginx nginx -s reload\n' \
  | sudo tee /etc/letsencrypt/renewal-hooks/deploy/nvzhuang-nginx-reload.sh >/dev/null
sudo chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/nvzhuang-nginx-reload.sh
sudo grep -E '^(authenticator|webroot_path)' "/etc/letsencrypt/renewal/$API_DOMAIN.conf"
sudo certbot renew --dry-run
systemctl list-timers --all | grep -E 'certbot|snap.certbot' 
```

预期：`nvz-acme-ok`；`authenticator = webroot` 与 `webroot_path = /opt/nvzhuang/deploy/nginx/acme,`；`Congratulations, all simulated renewals succeeded`；能看到 certbot 定时器。

若 `certbot reconfigure` 不可用（certbot < 2.3），用 `sudoedit /etc/letsencrypt/renewal/<API_DOMAIN>.conf` 把 `[renewalparams]` 中 `authenticator = standalone` 改为 `authenticator = webroot`，并追加 `webroot_path = /opt/nvzhuang/deploy/nginx/acme,` 以及 `[[webroot_map]]` 段 `<API_DOMAIN> = /opt/nvzhuang/deploy/nginx/acme`，再执行 `--dry-run`。

---

## 10. 验收测试

验收由 `deploy/verify.sh` 完成：每项输出 `PASS ...` 或 `FAIL ...`，最后一行 `验收结果: pass=N fail=M`，**任一 FAIL 退出码非 0**。Token 只从文件读取，不会出现在命令行和输出里。默认不调用付费模型（带 Token 的请求都会在调用上游前被参数校验、SSRF 校验或限流拦下，且这些请求不扣每日额度）。

> 脚本在 Nginx 的 `/tryon` 限流（20 次/分）下会刻意放慢节奏，单次运行约 1–2 分钟；连续两次运行之间等待 1 分钟，否则可能被自己的限流误伤。

### 10.1 服务器本机验收（含容器、日志、优雅重启）

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
sudo env API_DOMAIN="$API_DOMAIN" FRONTEND_ORIGIN="$FRONTEND_ORIGIN" FRONTEND_URL="${FRONTEND_URL:-$FRONTEND_ORIGIN/}" \
  TRYON_TOKEN_FILE=/etc/nvzhuang/verify.token VERIFY_CONNECT_IP=127.0.0.1 \
  bash deploy/verify.sh --local --restart 2>&1 | tee /tmp/nvz-verify-local.txt
echo "exit=${PIPESTATUS[0]}"
```

（`VERIFY_CONNECT_IP=127.0.0.1` 让请求经本机 443 进入 Nginx 容器，证书仍按 `API_DOMAIN` 严格校验；前端尚未部署时追加 `VERIFY_SKIP_FRONTEND=1`。）

预期：`exit=0`，末行 `验收结果: pass=<N> fail=0`。逐项对应如下（左列为需求，右列为脚本输出中必须出现的 `PASS` 行）：

| 验收项 | 脚本检查（必须为 PASS） | 实际判据 |
|---|---|---|
| HTTPS 与跳转 | `HTTPS 可访问（证书校验: 开启）`、`HTTP 301 跳转到 HTTPS` | `GET /hello` 返回 200 且证书有效；`http://` 返回 301 到 `https://` |
| 安全头 | `HSTS`、`X-Content-Type-Options`、`X-Frame-Options`、`Referrer-Policy`、`Permissions-Policy`、`Content-Security-Policy`、`隐藏 Nginx 版本（Server: nginx）` | 值与模板完全一致，如 `max-age=31536000`、`nosniff`、`DENY`、`no-referrer`、`default-src 'none'; frame-ancestors 'none'`；`Server` 不含版本号 |
| healthz/readyz | `本机 /healthz`、`本机 /readyz`、`readyz.config/lifecycle/models/state` | `/healthz` → `200 ok`；`/readyz` → `{"checks":{"config":"ok","lifecycle":"ok","models":"ok","state":"ok"},"status":"ready"}` |
| 无 Token 401 | `无 Token → 401 AUTH_REQUIRED`、`错误 Token → 401 AUTH_INVALID` | `{"code":40101,...,"error_code":"AUTH_REQUIRED"}` / `40102 AUTH_INVALID` |
| 带 Token 行为 | `有效 Token → 400 参数校验（鉴权通过）`、`错误响应带 request_id` | 空请求体 → HTTP 400，`"code":51`，`"message":"The person_image field is required"` |
| CORS | `CORS 预检：前端来源放行`、`CORS 预检：未授权来源 → 403 ORIGIN_NOT_ALLOWED` | `Access-Control-Allow-Origin` 等于 `FRONTEND_ORIGIN`；其它来源 `40301` |
| SSRF | `SSRF 拒绝 https://127.0.0.1`、`SSRF 拒绝 https://169.254.169.254` | HTTP 400，`"code":53`，`Base URL 不允许指向内网或保留地址` |
| 请求体上限 | `超大请求体 → 413 JSON` | 32 MB 请求体 → 413 `REQUEST_TOO_LARGE`（Nginx `41300`） |
| 429 | `连续请求触发 429 RATE_LIMITED（Retry-After: N）` | 连发 12 次，出现 429 且带 `Retry-After`（Nginx `42900`/10 秒或后端 `42901`） |
| 容器与资源 | `后端容器健康`、`Nginx 容器健康`、`后端未发布任何端口`、`Nginx 只发布 80/443`、`后端/Nginx 重启策略`、`后端内存上限 3 GiB / 3 CPU`、`GOMEMLIMIT=2GiB`、`后端 IP 172.30.0.10`、`Nginx IP 172.30.0.2` | `restart: unless-stopped`、`mem_limit 3g`、`cpus 3.0` |
| 配置一致性 | `生产配置已挂载（trustedProxies）`、`停机超时 190s`、`stop_grace_period 200s`、`日志轮转 20m×5` | 容器内 config 含 `172.30.0.2/32`、`timeoutSeconds: 190` |
| 日志脱敏 | `日志中不含 Token 明文`、`日志中不含 Bearer 头`、`日志中不含验收用假 Key`、`无 trustedProxies 部署告警` | 后端日志中不出现 Token、`Bearer <值>`、SSRF 测试里带的假 Key；无 `【部署告警】` |
| 优雅重启 | `后端退出码 0（非强杀）`、`停机用时 Ns < 200s`、`停机日志：收到停机信号`、`停机日志：HTTP 服务已关闭`、`后端停止时网关返回 JSON 错误（502 GATEWAY_ERROR 或 504 GATEWAY_TIMEOUT）`、`后端重新启动并健康`、`重启后 /readyz` | `docker stop -t 200` 后退出码 0；停机期间网关返回 JSON；90 秒内恢复 healthy |
| 额度持久化 | `额度文件 /app/data/quota.db 存在`、`重启后沿用同一额度文件（卷未丢失、未重建）`、`无损坏备份文件` | 重启前后 `quota.db` inode 相同、不存在 `quota.db.corrupt-*`、`readyz.state=ok` |

关于额度持久化的说明：`quota.db` 是 bbolt 文件，`/readyz` 的 state 检查与每次请求都会写入，所以不比较文件哈希，而是检查卷与文件在重启后被沿用；「计数值重启后不清零」由单元测试 `TestBoltQuotaStorePersistsAcrossRestart`（`backend/internal/security/quota_store_test.go`，`scripts/check.sh` 会运行）保证。

### 10.2 真实换装（推荐，消耗 1 次模型调用与 1 次额度）

仅当用户提供了 `VERIFY_PERSON_IMAGE`（本人同意使用的人像照片，jpg/png/webp，≥ 256×256）时执行；服装图默认用仓库里的 `assets/img/AUT-0001.webp`：

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
sleep 60
for m in $MODELS; do
  sudo env API_DOMAIN="$API_DOMAIN" FRONTEND_ORIGIN="$FRONTEND_ORIGIN" TRYON_TOKEN_FILE=/etc/nvzhuang/verify.token \
    VERIFY_CONNECT_IP=127.0.0.1 VERIFY_SKIP_FRONTEND=1 VERIFY_REAL_TRYON=1 VERIFY_MODEL="$m" \
    VERIFY_PERSON_IMAGE="$VERIFY_PERSON_IMAGE" bash deploy/verify.sh | grep -E '^(FAIL|PASS 真实换装|验收结果)'
  sleep 60
done
```

预期：每个模型都有 `PASS 真实换装成功（image/...，N 字节）` 和 `验收结果: pass=<N> fail=0`。`FAIL 真实换装: 状态码 [502] UPSTREAM_FAILED` 表示模型 Key/余额/网络问题（上游失败不退还额度）；看 `sudo docker logs costume-tryon-backend 2>&1 | tail -n 50`（已脱敏）。

### 10.3 外部验收（必须在服务器以外的机器执行）

服务器本机无法验证「公网访问探针被拒绝」。在用户电脑、备份节点或任意外网机器上（需要 bash、curl，以及同一 `DEPLOY_REF` 的仓库副本）：

```bash
# 先把 verify Token 放进本机 0600 文件（由用户从服务器 /etc/nvzhuang/verify.token 私下拷贝），不要粘贴到聊天里
API_DOMAIN=<API_DOMAIN> FRONTEND_ORIGIN=<FRONTEND_ORIGIN> FRONTEND_URL=<FRONTEND_URL> \
  TRYON_TOKEN_FILE=$HOME/.nvz-verify.token bash deploy/verify.sh; echo "exit=$?"
```

预期：`exit=0`、`fail=0`，且包含 `PASS 公网访问 /healthz 被拒绝` 与 `PASS 公网访问 /readyz 被拒绝`（HTTP 403）。如 AI 只能访问这台 VPS，请用户执行本节并回传最后 5 行输出。

### 10.4 前端接入

前端是静态站（仓库默认 GitHub Pages），不部署在本服务器：

1. 前端来源必须与 `security.trustedOrigins` 中的 `FRONTEND_ORIGIN` **完全一致**（协议+主机，无路径）。
2. 让前端指向本 API，二选一：
   - 每个用户在前端「设置中心」填写后端地址 `https://<API_DOMAIN>` 与自己的 Token（不需要改代码）；
   - 或在用户自己的前端仓库中修改 `config.js`：`window.AI_API_URL = 'https://<API_DOMAIN>';`（只接受 HTTPS，**禁止写入任何 Key/Token**），重新发布静态站后可用 `VERIFY_EXPECT_CONFIGJS=1` 让 verify.sh 检查。
3. 浏览器打开前端，选一件衣服做一次 AI 试穿，能出图即端到端可用。

---

## 11. 运维

### 11.1 备份 `/app/data` 与配置

额度数据在 Docker 卷 `nvzhuang_tryon-state`（容器内 `/app/data/quota.db`）。每天把它与配置、证书一起打包到 `/var/backups/nvzhuang`（0700，保留 14 天）：

```bash
sudo tee /usr/local/sbin/nvzhuang-backup.sh >/dev/null <<'EOF2'
#!/bin/sh
# nvzhuang 每日备份：额度库 + 配置 + 证书。归档含机密（.env、私钥），只允许 root 读取。
set -eu
umask 077
D=/var/backups/nvzhuang; TS=$(date -u +%Y%m%dT%H%M%SZ); W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT
install -d -m 0700 "$D"
docker cp costume-tryon-backend:/app/data/quota.db "$W/quota.db"
cp /opt/nvzhuang/.env "$W/root.env"
cp /opt/nvzhuang/deploy/.env.prod "$W/env.prod"
cp /opt/nvzhuang/backend/manifest/config/config.yaml "$W/config.yaml"
git -C /opt/nvzhuang rev-parse HEAD > "$W/DEPLOY_SHA"
cp -a /etc/nvzhuang "$W/etc-nvzhuang"
[ -d /etc/letsencrypt ] && cp -a /etc/letsencrypt "$W/letsencrypt"
tar -C "$W" -czf "$D/nvzhuang-$TS.tar.gz" .
find "$D" -name 'nvzhuang-*.tar.gz' -mtime +14 -delete
EOF2
sudo chmod 0700 /usr/local/sbin/nvzhuang-backup.sh
echo '17 3 * * * root /usr/local/sbin/nvzhuang-backup.sh' | sudo tee /etc/cron.d/nvzhuang-backup >/dev/null
sudo /usr/local/sbin/nvzhuang-backup.sh && sudo ls -l /var/backups/nvzhuang/
sudo tar -tzf "$(sudo ls -1t /var/backups/nvzhuang/nvzhuang-*.tar.gz | head -1)" | grep -E '^\./(quota\.db|config\.yaml|root\.env|DEPLOY_SHA)$'
```

预期：出现 `nvzhuang-<时间>.tar.gz`，权限 `-rw-------`；最后一条列出 4 个文件名。

- 异地保存：由备份节点（`BACKUP_PULL_HOST`）每天经 SSH 用 `ADMIN_USER` 拉取（`sudo` 读取后传输），落地后同样 0600；归档含机密，不要放到网盘或对象存储的公开位置。
- 恢复额度库：`dc stop costume-tryon && sudo docker cp quota.db costume-tryon-backend:/app/data/quota.db && dc start costume-tryon`（额度数据价值低，丢失只会让当天计数归零）。运行中复制的 bbolt 文件偶有不一致，后端发现损坏会改名为 `quota.db.corrupt-<时间>` 并重建。

### 11.2 日志轮转

- 容器日志：compose 为两个容器设置了 `json-file` `max-size: 20m`、`max-file: 5`（`deploy/verify.sh --local` 的 `日志轮转 20m×5` 已验证），`init-host.sh` 也为 Docker 守护进程设置了同样的默认值。Nginx 访问/错误日志输出到容器标准输出，受同一限制。
- 查看：`sudo docker logs --since 1h costume-tryon-backend`、`sudo docker logs --since 1h costume-tryon-nginx`。
- 宿主机 journald 建议限额：`sudo mkdir -p /etc/systemd/journald.conf.d && printf '[Journal]\nSystemMaxUse=500M\n' | sudo tee /etc/systemd/journald.conf.d/90-nvzhuang.conf >/dev/null && sudo systemctl restart systemd-journald`。

### 11.3 开机自启（必须实际重启验证）

两个容器都是 `restart: unless-stopped`，Docker 服务已 `enable`。实际重启一次：

```bash
systemctl is-enabled docker containerd
sudo reboot
```

重新 SSH 登录（约 1–2 分钟）后：

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
uptime -s
sleep 60
sudo env API_DOMAIN="$API_DOMAIN" FRONTEND_ORIGIN="$FRONTEND_ORIGIN" FRONTEND_URL="${FRONTEND_URL:-$FRONTEND_ORIGIN/}" \
  TRYON_TOKEN_FILE=/etc/nvzhuang/verify.token VERIFY_CONNECT_IP=127.0.0.1 bash deploy/verify.sh --local | tail -n 1
```

预期：`enabled` ×2；`uptime -s` 为刚才的时间；末行 `验收结果: pass=<N> fail=0`。

> 手工 `docker stop`/`dc stop` 的容器重启后**不会**自动拉起（`unless-stopped` 语义）；维护结束务必 `dc up -d`。

### 11.4 升级

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
NEW_REF=<新的 tag 或完整 SHA>
PREV=$(git rev-parse HEAD); echo "$PREV" | sudo tee /etc/nvzhuang/previous-sha >/dev/null
sudo docker image tag nvzhuang-costume-tryon:latest "nvzhuang-costume-tryon:rollback-${PREV:0:12}"
sudo /usr/local/sbin/nvzhuang-backup.sh
git fetch --tags origin && git -c advice.detachedHead=false checkout --detach "$NEW_REF"
git diff --stat "$PREV" HEAD -- backend/manifest/config/config.example.yaml deploy/ docker-compose.yml
dc config --quiet && dc build --pull && dc up -d
sleep 60
sudo env API_DOMAIN="$API_DOMAIN" FRONTEND_ORIGIN="$FRONTEND_ORIGIN" FRONTEND_URL="${FRONTEND_URL:-$FRONTEND_ORIGIN/}" \
  TRYON_TOKEN_FILE=/etc/nvzhuang/verify.token VERIFY_CONNECT_IP=127.0.0.1 bash deploy/verify.sh --local | tail -n 1
```

- `git diff --stat` 若显示 `config.example.yaml` 或 `deploy/` 有变化：先阅读变更，把新增的配置键同步进服务器上的 `config.yaml`（§5.5）/`.env.prod`，再 `dc up -d`。
- `dc up -d` 重建后端时，在途请求最多等待 190 秒完成（优雅停机）；建议在低峰执行。
- 末行不是 `fail=0`：立即回滚（§11.5）。

### 11.5 回滚

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
PREV=$(cat /etc/nvzhuang/previous-sha)
git -c advice.detachedHead=false checkout --detach "$PREV"
sudo docker image tag "nvzhuang-costume-tryon:rollback-${PREV:0:12}" nvzhuang-costume-tryon:latest
dc up -d --no-build
sleep 60
sudo env API_DOMAIN="$API_DOMAIN" FRONTEND_ORIGIN="$FRONTEND_ORIGIN" FRONTEND_URL="${FRONTEND_URL:-$FRONTEND_ORIGIN/}" \
  TRYON_TOKEN_FILE=/etc/nvzhuang/verify.token VERIFY_CONNECT_IP=127.0.0.1 bash deploy/verify.sh --local | tail -n 1
```

预期：`git rev-parse HEAD` 等于 `PREV`，验收 `fail=0`。若升级时改过 `config.yaml`，从 §11.1 的备份中恢复旧版 `config.yaml` 后再 `dc up -d --no-build`。确认稳定后清理旧镜像：`sudo docker image prune -f`。

### 11.6 监控（可选）

外部可用性监控（交给 `ALERT_CONTACT`）只探测 `https://<API_DOMAIN>/hello`（公网可访问、不鉴权、不调用模型）；`/healthz`、`/readyz` 对公网返回 403，属预期。需要就绪状态时在服务器本机执行：`curl -s --resolve "$API_DOMAIN:443:127.0.0.1" "https://$API_DOMAIN/readyz"`。

---

## 12. 故障排查

| 现象 | 原因 | 处理 |
|---|---|---|
| `dc config` 报 `!reset` 相关错误 | Compose < 2.24 | 按 §3.5 安装官方 `docker-compose-plugin` |
| `env file .../.env not found` | 根目录 `.env` 未创建 | §5.3 |
| `请在 .env.prod 中设置 API_SERVER_NAME`（或 TLS_*、NVZHUANG_CONFIG_PATH） | `deploy/.env.prod` 缺项，或没用 `dc`（缺 `--env-file deploy/.env.prod`） | §5.4，始终用 `dc` |
| `/readyz` 的 `models` 不是 ok（503），或换装报模型未配置 | Key 插值成空：Key 写在 `backend/.env`、或只用了一个 `--env-file`、或 `config.yaml` 启用了没有 Key 的模型 | §5.3/§5.6 检查 `set/empty`，修正后 `dc up -d` |
| 后端退出，日志 `环境变量 TRYON_API_KEYS 中包含示例/占位 Token` | Token 太短/低熵/含占位词 | §5.2 用 `openssl rand -hex 32` 重新生成，§5.3 重写 `.env`，`dc up -d` |
| 后端退出，日志含 `config.yaml` 与 `permission denied` | 配置文件权限不允许 UID/GID 10001 读取 | `sudo chown root:10001 backend/manifest/config/config.yaml && sudo chmod 0640 ...` |
| 后端退出，日志 `创建状态目录失败`/`打开额度数据库失败` | `tryon-state` 卷异常或磁盘满 | `df -h`；`sudo docker volume inspect nvzhuang_tryon-state` |
| Nginx 容器反复重启，`cannot load certificate` | `TLS_CERT_PATH`/`TLS_KEY_PATH` 错误，或 `TLS_DIR` 没有覆盖证书真实文件（Let's Encrypt 的 `live/` 是指向 `archive/` 的软链接，必须挂载整个 `/etc/letsencrypt`） | §5.4、§7 |
| `bind: address already in use` 80/443 | 宿主机 nginx/apache/caddy 仍在运行 | §2.3 |
| certbot `Timeout during connect`/`unauthorized` | DNS 未指向本机、云安全组未放行 80、或存在错误的 AAAA | §2.4、§2.3；`--webroot` 时确认 §9.2 的 `nvz-acme-ok` |
| 新 SSH 会话登录失败 | 来源不在 `SSH_ALLOW_CIDR`、公钥错误、云安全组未放行新端口 | 用旧会话或云厂商 VNC 控制台：删除 `/etc/ssh/sshd_config.d/10-nvzhuang-hardening.conf`、`sudo ufw disable`，修正后重跑 §6 |
| 浏览器报 CORS / `403 ORIGIN_NOT_ALLOWED`（40301） | 前端来源与 `trustedOrigins` 不完全一致（多了路径或斜杠、http/https 不同） | 修正 §5.5 的 `FRONTEND_ORIGIN`，`dc up -d --force-recreate costume-tryon` |
| 前端提示混合内容 / 地址无效 | 后端地址不是 `https://` | 设置中心或 `config.js` 只能填 `https://<API_DOMAIN>` |
| `401 40101 AUTH_REQUIRED` / `40102 AUTH_INVALID` | 未带 Token / Token 不在 `TRYON_API_KEYS` 中 | 确认用户填写的 Token 来自 `/etc/nvzhuang/tokens.tsv`；改 `.env` 后需 `dc up -d` |
| `429 42900`（`Retry-After: 10`） | Nginx 按 IP 限流（`/tryon` 20 次/分、突发 5、单 IP 并发 2） | 正常保护；如需调整改模板后 `dc up -d --force-recreate nginx` |
| `429 42901` / `42902` / `42903` | 后端令牌桶（10 次/分、突发 3）/ 每日额度（100 次，UTC 0 点重置）/ 并发（全局 6、每人 1，`Retry-After: 5`） | 正常保护；按需调整 `config.yaml` 的 `security.rateLimit` / `concurrency` |
| 所有用户共用一个限流桶，或公网能访问 `/readyz` | Nginx 被改成同时发布 IPv6（`[::]:80/443`），IPv6 连接被 Docker 用户态代理转成 `172.30.0.1`；或后端被发布了端口 | 恢复 compose 中的 `0.0.0.0:` 端口写法并删除 AAAA（§2.4）；`dc ps` 确认后端无端口 |
| 用 IP 或其它域名访问时连接被直接断开 | 正常：80/443 的 `default_server` 对未知 Host / 无 SNI 返回 444 | 用 API 域名访问 |
| 日志出现 `【部署告警】收到来自私有地址 ...` | 有流量绕过 Nginx 直达后端，或 `trustedProxies` 被改 | 恢复 `trustedProxies: ["172.30.0.2/32"]`，确认后端无端口映射 |
| `502 50200 GATEWAY_ERROR` | 后端未运行/重启中 | `dc ps`、`sudo docker logs costume-tryon-backend` |
| `502 UPSTREAM_FAILED` | 模型 Key 无效、余额不足、上游超时或网络不通 | 看后端日志（已脱敏）；§2.5 检查出站 |
| `504 50400 GATEWAY_TIMEOUT` | 单次生成超过 Nginx `proxy_read_timeout 300s` | 正常情况下后端 180 秒先超时；持续出现说明上游极慢，换模型或稍后重试 |
| `413 41300 REQUEST_TOO_LARGE` | `/tryon` 请求体 > 30 MB、其它路径 > 64 KB（Nginx），或 > 24 MiB（后端 41301） | 前端会自动勾选「上传前压缩」，让用户重试 |
| 出现 `quota.db.corrupt-<时间>` | 额度库损坏后被自动重建（当天计数归零） | 检查磁盘与异常断电；无需其他处理 |
| verify.sh 某项因 429 失败 | 短时间内重复运行，被自己的限流命中 | 等 1 分钟后重跑 |

---

## 13. 完成定义（机器可检查）

以下**全部**满足才算交付完成。AI 在服务器上执行这段，输出必须是 `DONE`：

```bash
source /etc/nvzhuang/inputs.env && cd /opt/nvzhuang
ok=1; chk() { if eval "$2" >/dev/null 2>&1; then echo "OK   $1"; else echo "MISS $1"; ok=0; fi; }
chk "代码在固定版本"         '[ "$(git rev-parse HEAD)" = "$(git rev-parse "$DEPLOY_REF^{commit}")" ] && [ -z "$(git status --porcelain)" ]'
chk "SSH 仅公钥/禁 root"      'sudo sshd -T | grep -qx "passwordauthentication no" && sudo sshd -T | grep -qx "permitrootlogin no"'
chk "ufw 已启用"              'sudo ufw status | grep -q "Status: active"'
chk "Docker 开机自启"         '[ "$(systemctl is-enabled docker)" = enabled ]'
chk "机密文件 0600"           '[ "$(sudo stat -c %a .env /etc/nvzhuang/tokens.tsv /etc/nvzhuang/model-keys.env | sort -u)" = 600 ]'
chk "config.yaml 640 root:10001" '[ "$(sudo stat -c "%a %U %g" backend/manifest/config/config.yaml)" = "640 root 10001" ]'
chk "证书有效 ≥ 7 天"          'sudo openssl x509 -in "$(grep ^TLS_CERT_PATH= deploy/.env.prod | cut -d= -f2-)" -noout -checkend 604800'
chk "续期演练通过"            '[ "$CERT_MODE" != letsencrypt ] || sudo certbot renew --dry-run'
chk "已有备份归档"            'sudo find /var/backups/nvzhuang -name "nvzhuang-*.tar.gz" -mmin -1500 | grep -q .'
chk "备份定时任务"            'test -f /etc/cron.d/nvzhuang-backup'
chk "重启后验证过"            '[ "$(date -d "$(uptime -s)" +%s)" -gt "$(stat -c %Y /etc/nvzhuang/inputs.env)" ]'
chk "本机验收 fail=0"         'sudo env API_DOMAIN="$API_DOMAIN" FRONTEND_ORIGIN="$FRONTEND_ORIGIN" FRONTEND_URL="${FRONTEND_URL:-$FRONTEND_ORIGIN/}" TRYON_TOKEN_FILE=/etc/nvzhuang/verify.token VERIFY_CONNECT_IP=127.0.0.1 bash deploy/verify.sh --local'
[ "$ok" = 1 ] && echo DONE || echo NOT_DONE
```

另外两项需要人工/外部确认后写入交付报告：
- §10.3 外部验收输出 `fail=0` 且包含 `公网访问 /healthz 被拒绝`、`公网访问 /readyz 被拒绝`；
- §10.1 的 `--restart` 运行（优雅重启）`fail=0`，若用户提供了人像照片，§10.2 每个模型 `PASS 真实换装成功`。

交付报告应包含：部署的 commit SHA、`API_DOMAIN`、启用的模型、Token 名单（只写名字，不写值）与取回方式（`sudo cat /etc/nvzhuang/tokens.tsv`）、三次验收（本机含重启、重启后、外部）的末行输出、证书到期时间、备份位置。

---

## 附录 A：4c8g 热备节点（DNS 故障切换）做成一致副本

目标：热备与主节点运行**完全相同**的代码、镜像与配置，DNS 切过去即可服务。

1. **同样的输入与步骤**：在热备上按 §2–§6 执行，`inputs.env` 与主节点相同（只改 `SERVER_PUBLIC_IP=<STANDBY_IP>`），`DEPLOY_REF` 必须相同。§2.2 资源要求按 8 GiB 内存检查即可（后端上限 3 GiB + Nginx 256 MiB，配置无需修改）。
2. **同一份机密与配置**：从主节点复制，不要重新生成（否则用户 Token 不通用）。在主节点执行，经 SSH 传输，不落地到第三方：
   ```bash
   sudo tar -C / -czf - opt/nvzhuang/.env opt/nvzhuang/deploy/.env.prod opt/nvzhuang/backend/manifest/config/config.yaml \
     etc/nvzhuang/tokens.tsv etc/nvzhuang/verify.token etc/nvzhuang/model-keys.env \
     | ssh -p <SSH_PORT> <ADMIN_USER>@<STANDBY_IP> 'sudo tar -C / -xzpf -'
   ```
   随后在热备上复查权限：`.env`、`tokens.tsv`、`model-keys.env`、`verify.token` 为 `600 root`，`config.yaml` 为 `640 root 10001`。
3. **同一个镜像**（不在热备重新构建，避免依赖漂移）：
   ```bash
   # 主节点
   sudo docker save nvzhuang-costume-tryon:latest nginx:1.28-alpine | gzip | ssh -p <SSH_PORT> <ADMIN_USER>@<STANDBY_IP> 'gunzip | sudo docker load'
   # 两边分别执行，输出必须一致
   sudo docker image inspect -f '{{.Id}}' nvzhuang-costume-tryon:latest nginx:1.28-alpine
   ```
   热备上用 `dc up -d --no-build` 启动（不要 `build`）。
4. **证书**：DNS 指向主节点时热备无法通过 HTTP-01 签发。二选一：
   - 主节点每次续期后同步：在主节点的 `/etc/letsencrypt/renewal-hooks/deploy/` 增加一个脚本，把 `/etc/letsencrypt` 经 `tar | ssh` 推送到热备并执行 `docker exec costume-tryon-nginx nginx -s reload`（需要主节点到热备的 SSH 免密密钥，仅用于此用途）；首次手工同步一次；
   - 或两边都改用 DNS-01 方式各自签发（需要 DNS 服务商的 API 凭据，按 certbot 对应插件文档配置）。
   热备上**不要**运行 standalone/webroot 签发，也不要启用 §9.2 的切换步骤。
5. **验收**：在热备本机执行 §10.1（`VERIFY_CONNECT_IP=127.0.0.1`），在外部执行 §10.3 并加 `VERIFY_CONNECT_IP=<STANDBY_IP>`（不改 DNS 即可验证热备），都必须 `fail=0`；再做一次 §11.3 实际重启验证。
6. **DNS 故障切换**：API 域名 A 记录 TTL 设为 60 秒，健康检查探测 `https://<API_DOMAIN>/hello`（期望 200）；主节点失败时切到 `<STANDBY_IP>`。不要配 AAAA。
7. **已知差异**：每日额度计数按节点各自保存（切换当天用户在热备上会重新计数，最多相当于额度翻倍）；升级时两边按 §11.4 用同一个 `NEW_REF`，并重复第 3 步同步镜像，再分别 `dc up -d --no-build` 与验收。
