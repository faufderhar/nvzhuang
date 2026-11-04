#!/usr/bin/env bash
# 把主节点的每日额度快照推送到热备节点（附录 A 第 7 步），避免 DNS 故障切换当天额度翻倍。
# 在主节点以 root 运行（cron 每 5 分钟一次）：
#   */5 * * * * root /opt/nvzhuang/deploy/host/quota-sync.sh <ADMIN_USER>@<STANDBY_IP> <SSH_PORT> >/dev/null 2>&1
# 只传输 quota.db（内容只有主体哈希与计数，不含 Token 或个人信息），经 SSH 传输。
# 热备后端每 30 秒检查一次 quota-import.db，按「取较大值」合并今天和昨天的计数后改名为 .merged-<时间戳>。
set -euo pipefail

TARGET="${1:?用法: quota-sync.sh <user>@<standby_ip> [ssh_port]}"
PORT="${2:-22}"
CONTAINER="${QUOTA_SYNC_CONTAINER:-costume-tryon-backend}"

data_dir() {
  docker inspect -f '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Source}}{{end}}{{end}}' "$CONTAINER"
}

SRC_DIR="$(data_dir)"
[ -n "$SRC_DIR" ] && [ -f "$SRC_DIR/quota.db" ] || { echo "未找到 $CONTAINER 的 /app/data/quota.db（是否设置了 security.stateDir？）" >&2; exit 1; }

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
# bbolt 写入时持有文件排他锁；共享锁下复制可得到一致快照（最多等 10 秒）
flock -s -w 10 "$SRC_DIR/quota.db" cp "$SRC_DIR/quota.db" "$TMP"

# 远端：写入临时名后原子改名，属主与后端容器用户一致（UID 10001）
# shellcheck disable=SC2016
ssh -p "$PORT" -o BatchMode=yes "$TARGET" "sudo bash -c '
  set -euo pipefail
  dir=\$(docker inspect -f \"{{range .Mounts}}{{if eq .Destination \\\"/app/data\\\"}}{{.Source}}{{end}}{{end}}\" $CONTAINER)
  [ -n \"\$dir\" ] || { echo 热备未找到 /app/data 卷 >&2; exit 1; }
  cat > \"\$dir/quota-import.db.tmp\"
  chown 10001:10001 \"\$dir/quota-import.db.tmp\"
  chmod 600 \"\$dir/quota-import.db.tmp\"
  mv -f \"\$dir/quota-import.db.tmp\" \"\$dir/quota-import.db\"
'" < "$TMP"
