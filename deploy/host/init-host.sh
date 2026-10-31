#!/usr/bin/env bash
# nvzhuang 生产主机初始化（Debian/Ubuntu）。默认只打印将要执行的操作（dry-run），加 --apply 才真正修改系统。
#
# 必填环境变量（不要把真实值提交到仓库）：
#   ADMIN_USER       已存在且配置了 SSH 公钥的管理用户（脚本会先检查，防止关闭密码登录后被锁在门外）
#   SSH_ALLOW_CIDR   允许 SSH 登录的来源地址/网段，例如你的固定出口 IP/32 或 WireGuard 网段
# 可选环境变量：
#   SSH_PORT=22            SSH 端口
#   ENABLE_WIREGUARD=0     设为 1 时安装 wireguard-tools 并放行 WG_PORT/udp（不会生成或写入任何密钥配置）
#   WG_PORT=51820
#   HOST_TIMEZONE=UTC
#
# 用法：
#   sudo ADMIN_USER=deploy SSH_ALLOW_CIDR=198.51.100.7/32 bash deploy/host/init-host.sh           # 预览
#   sudo ADMIN_USER=deploy SSH_ALLOW_CIDR=198.51.100.7/32 bash deploy/host/init-host.sh --apply   # 执行
# 注意：Docker 发布的端口会绕过 ufw。生产 compose 只发布 Nginx 的 80/443，后端不发布端口。
set -euo pipefail

APPLY=0
for arg in "$@"; do
  case "$arg" in
    --apply) APPLY=1 ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "未知参数: $arg" >&2; exit 2 ;;
  esac
done

SSH_PORT="${SSH_PORT:-22}"
ENABLE_WIREGUARD="${ENABLE_WIREGUARD:-0}"
WG_PORT="${WG_PORT:-51820}"
HOST_TIMEZONE="${HOST_TIMEZONE:-UTC}"

die() { echo "错误: $*" >&2; exit 1; }
log() { echo "==> $*"; }
run() {
  if [ "$APPLY" -eq 1 ]; then "$@"; else printf '[dry-run]'; printf ' %q' "$@"; printf '\n'; fi
}
# write_file <路径> <权限>，内容来自标准输入；dry-run 时只打印
write_file() {
  local path="$1" mode="$2" content
  content="$(cat)"
  if [ "$APPLY" -eq 1 ]; then
    install -d -m 0755 "$(dirname "$path")"
    printf '%s\n' "$content" > "$path.tmp.$$"
    chmod "$mode" "$path.tmp.$$"
    mv "$path.tmp.$$" "$path"
  else
    echo "[dry-run] 写入 $path (mode $mode):"
    printf '%s\n' "$content" | sed 's/^/    | /'
  fi
}

valid_port() { [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]; }
valid_cidr() {
  [[ "$1" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}(/([0-9]|[12][0-9]|3[0-2]))?$ ]] ||
    [[ "$1" =~ ^[0-9a-fA-F:]+(/([0-9]|[1-9][0-9]|1[01][0-9]|12[0-8]))?$ ]]
}

# ---------- 参数校验 ----------
[ -n "${ADMIN_USER:-}" ] || die "请设置 ADMIN_USER"
[ -n "${SSH_ALLOW_CIDR:-}" ] || die "请设置 SSH_ALLOW_CIDR"
valid_cidr "$SSH_ALLOW_CIDR" || die "SSH_ALLOW_CIDR 格式无效: $SSH_ALLOW_CIDR"
case "$SSH_ALLOW_CIDR" in 0.0.0.0/0|::/0) die "SSH_ALLOW_CIDR 不能是全网段" ;; esac
valid_port "$SSH_PORT" || die "SSH_PORT 无效: $SSH_PORT"
valid_port "$WG_PORT" || die "WG_PORT 无效: $WG_PORT"
[[ "$ENABLE_WIREGUARD" =~ ^[01]$ ]] || die "ENABLE_WIREGUARD 只能是 0 或 1"
[[ "$HOST_TIMEZONE" =~ ^[A-Za-z0-9_+/-]+$ ]] || die "HOST_TIMEZONE 无效"
[[ "$ADMIN_USER" =~ ^[a-z_][a-z0-9_-]{0,31}$ ]] || die "ADMIN_USER 无效"

if [ "$APPLY" -eq 1 ]; then
  [ "$(id -u)" -eq 0 ] || die "--apply 需要 root 权限"
  command -v apt-get >/dev/null || die "仅支持 Debian/Ubuntu（apt-get）"
  id "$ADMIN_USER" >/dev/null 2>&1 || die "用户 $ADMIN_USER 不存在"
  home_dir="$(getent passwd "$ADMIN_USER" | cut -d: -f6)"
  [ -s "$home_dir/.ssh/authorized_keys" ] || die "$ADMIN_USER 没有 ~/.ssh/authorized_keys，关闭密码登录会导致无法登录，已中止"
fi

if [ -n "${SSH_CONNECTION:-}" ]; then
  log "当前 SSH 会话来源: ${SSH_CONNECTION%% *}。请确认它包含在 SSH_ALLOW_CIDR=$SSH_ALLOW_CIDR 内，否则启用防火墙后将无法再次登录"
fi

# ---------- 1. 软件包 ----------
log "安装基础软件包"
pkgs=(ufw fail2ban unattended-upgrades chrony ca-certificates curl)
[ "$ENABLE_WIREGUARD" = 1 ] && pkgs+=(wireguard-tools)
run env DEBIAN_FRONTEND=noninteractive apt-get update -q
run env DEBIAN_FRONTEND=noninteractive apt-get install -y -q "${pkgs[@]}"

# ---------- 2. 自动安全更新 ----------
log "启用 unattended-upgrades"
write_file /etc/apt/apt.conf.d/20auto-upgrades 0644 <<'CONF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
CONF

# ---------- 3. 时间同步 ----------
log "时区 $HOST_TIMEZONE + chrony 时间同步"
run timedatectl set-timezone "$HOST_TIMEZONE"
run systemctl enable --now chrony

# ---------- 4. SSH 加固 ----------
log "SSH：仅公钥登录，禁止 root 登录，端口 $SSH_PORT"
write_file /etc/ssh/sshd_config.d/10-nvzhuang-hardening.conf 0644 <<CONF
Port $SSH_PORT
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
PermitEmptyPasswords no
MaxAuthTries 3
LoginGraceTime 30
X11Forwarding no
AllowUsers $ADMIN_USER
CONF
run sshd -t
# Ubuntu 22.10+ 默认使用 ssh.socket 套接字激活，端口变更需要重启 socket
if systemctl is-active --quiet ssh.socket 2>/dev/null; then
  run systemctl daemon-reload
  run systemctl restart ssh.socket
elif systemctl list-unit-files ssh.service >/dev/null 2>&1; then
  run systemctl reload ssh
else
  run systemctl reload sshd
fi

# ---------- 5. 防火墙 ----------
log "ufw：默认拒绝入站；SSH 仅允许 $SSH_ALLOW_CIDR；开放 80/443"
run ufw default deny incoming
run ufw default allow outgoing
run ufw allow from "$SSH_ALLOW_CIDR" to any port "$SSH_PORT" proto tcp
run ufw allow 80/tcp
run ufw allow 443/tcp
[ "$ENABLE_WIREGUARD" = 1 ] && run ufw allow "$WG_PORT"/udp
run ufw --force enable

# ---------- 6. fail2ban ----------
log "fail2ban：SSH 暴力破解防护"
write_file /etc/fail2ban/jail.d/nvzhuang-sshd.local 0644 <<CONF
[sshd]
enabled = true
port = $SSH_PORT
backend = systemd
maxretry = 5
findtime = 10m
bantime = 1h
CONF
run systemctl enable --now fail2ban
run systemctl restart fail2ban

# ---------- 7. Docker 日志上限 ----------
if [ -e /etc/docker/daemon.json ]; then
  log "/etc/docker/daemon.json 已存在，未覆盖。请确认包含 log-opts max-size/max-file 限制"
else
  log "Docker：json-file 日志单文件 20m、保留 5 个"
  write_file /etc/docker/daemon.json 0644 <<'CONF'
{
  "log-driver": "json-file",
  "log-opts": { "max-size": "20m", "max-file": "5" },
  "live-restore": true
}
CONF
  log "如 Docker 已安装，请在维护窗口执行 systemctl restart docker 使其生效"
fi

# ---------- 8. WireGuard（可选，仅安装与放行端口） ----------
if [ "$ENABLE_WIREGUARD" = 1 ]; then
  log "WireGuard 已安装并放行 ${WG_PORT}/udp。密钥与 /etc/wireguard/wg0.conf 请在主机上手动生成，不要提交到仓库"
fi

if [ "$APPLY" -eq 1 ]; then
  log "完成。请保持当前 SSH 会话，另开终端验证能用公钥登录端口 $SSH_PORT 后再退出"
else
  log "以上为预览，未做任何修改。确认无误后加 --apply 执行"
fi
