#!/usr/bin/env bash
# nvzhuang 上线验收脚本：对已部署的 API 域名执行一组只读/低成本检查，输出 PASS/FAIL，任一失败则非零退出。
# 默认不调用任何付费模型：所有带 Token 的请求都会在调用上游之前被参数校验、SSRF 校验或限流拦下（VERIFY_REAL_TRYON=1 除外）。
# Token 只从文件或环境变量读取，写入 0600 临时文件后以 curl -H @文件 传递，不出现在命令行参数和输出中。
#
# 必填环境变量：
#   API_DOMAIN         对外 API 域名，例如 api.example.com（不带协议）
#   FRONTEND_ORIGIN    前端来源（必须在 security.trustedOrigins 中），例如 https://frontend.example.com
#   TRYON_TOKEN_FILE   只含一个后端访问 Token 的文件路径（首行）；或改用 TRYON_TOKEN 环境变量
# 可选环境变量：
#   FRONTEND_URL=${FRONTEND_ORIGIN}/   前端页面地址；VERIFY_SKIP_FRONTEND=1 跳过前端检查
#   VERIFY_EXPECT_CONFIGJS=0           设为 1 时要求前端 config.js 中包含 https://${API_DOMAIN}
#   VERIFY_CONNECT_IP                  强制连接到该 IP（curl --resolve），用于 DNS 尚未生效或在服务器本机验证
#   VERIFY_PORT=443  VERIFY_HTTP_PORT=80（设为 0 跳过 HTTP→HTTPS 跳转检查）
#   VERIFY_INSECURE=0                  设为 1 时不校验证书（仅限本地测试，生产验收不得使用）
#   VERIFY_REAL_TRYON=0                设为 1 时真实生成一次换装图（会调用一次付费模型、扣 1 次额度），
#                                      需同时设置 VERIFY_PERSON_IMAGE（人像，jpg/png/webp，≥256×256），
#                                      可选 VERIFY_GARMENT_IMAGE（默认 assets/img/AUT-0001.webp）、VERIFY_MODEL（默认 gemini）
#
# 用法（在仓库根目录）：
#   API_DOMAIN=api.example.com FRONTEND_ORIGIN=https://frontend.example.com TRYON_TOKEN_FILE=/root/verify.token deploy/verify.sh
#   sudo -E deploy/verify.sh --local            # 在服务器上执行：追加探针、容器、日志脱敏、额度文件检查
#   sudo -E deploy/verify.sh --local --restart  # 追加优雅停机与额度持久化检查（会让后端停机约 10~30 秒）
set -uo pipefail

LOCAL=0
RESTART=0
for arg in "$@"; do
  case "$arg" in
    --local) LOCAL=1 ;;
    --restart) RESTART=1; LOCAL=1 ;;
    -h|--help) sed -n '2,23p' "$0"; exit 0 ;;
    *) echo "未知参数: $arg" >&2; exit 2 ;;
  esac
done

die() { echo "错误: $*" >&2; exit 2; }

API_DOMAIN="${API_DOMAIN:-}"
FRONTEND_ORIGIN="${FRONTEND_ORIGIN:-}"
VERIFY_PORT="${VERIFY_PORT:-443}"
VERIFY_HTTP_PORT="${VERIFY_HTTP_PORT:-80}"
VERIFY_INSECURE="${VERIFY_INSECURE:-0}"
VERIFY_SKIP_FRONTEND="${VERIFY_SKIP_FRONTEND:-0}"
VERIFY_EXPECT_CONFIGJS="${VERIFY_EXPECT_CONFIGJS:-0}"
VERIFY_CONNECT_IP="${VERIFY_CONNECT_IP:-}"
VERIFY_REAL_TRYON="${VERIFY_REAL_TRYON:-0}"
VERIFY_MODEL="${VERIFY_MODEL:-gemini}"

[[ "$API_DOMAIN" =~ ^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$ ]] || die "API_DOMAIN 无效或未设置（只填域名，例如 api.example.com）"
[[ "$FRONTEND_ORIGIN" =~ ^https://[a-zA-Z0-9.-]+(:[0-9]{1,5})?$ ]] || die "FRONTEND_ORIGIN 必须是 https://主机[:端口]，不带路径"
FRONTEND_URL="${FRONTEND_URL:-$FRONTEND_ORIGIN/}"
[[ "$FRONTEND_URL" =~ ^https://[^[:space:]]+$ ]] || die "FRONTEND_URL 必须使用 HTTPS"
[[ "$VERIFY_PORT" =~ ^[0-9]{1,5}$ ]] || die "VERIFY_PORT 无效"
[[ "$VERIFY_HTTP_PORT" =~ ^[0-9]{1,5}$ ]] || die "VERIFY_HTTP_PORT 无效"
if [ -n "$VERIFY_CONNECT_IP" ]; then
  [[ "$VERIFY_CONNECT_IP" =~ ^[0-9.]+$|^[0-9a-fA-F:]+$ ]] || die "VERIFY_CONNECT_IP 必须是 IP 地址"
fi
[[ "$VERIFY_MODEL" =~ ^[A-Za-z0-9._-]{1,64}$ ]] || die "VERIFY_MODEL 无效"
if [ "$VERIFY_REAL_TRYON" = 1 ]; then
  VERIFY_GARMENT_IMAGE="${VERIFY_GARMENT_IMAGE:-assets/img/AUT-0001.webp}"
  [ -r "${VERIFY_PERSON_IMAGE:-}" ] || die "VERIFY_REAL_TRYON=1 需要可读的 VERIFY_PERSON_IMAGE"
  [ -r "$VERIFY_GARMENT_IMAGE" ] || die "无法读取 VERIFY_GARMENT_IMAGE"
  command -v base64 >/dev/null 2>&1 || die "需要 base64"
fi
command -v curl >/dev/null 2>&1 || die "需要 curl"

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
chmod 700 "$T"

# ---- 读取 Token（不打印） ----
TOKEN=""
if [ -n "${TRYON_TOKEN_FILE:-}" ]; then
  [ -r "$TRYON_TOKEN_FILE" ] || die "无法读取 TRYON_TOKEN_FILE"
  IFS= read -r TOKEN < "$TRYON_TOKEN_FILE" || true
elif [ -n "${TRYON_TOKEN:-}" ]; then
  TOKEN="$TRYON_TOKEN"
fi
TOKEN="${TOKEN//$'\r'/}"
if [ "${#TOKEN}" -lt 32 ] || [ "${#TOKEN}" -gt 512 ]; then die "未提供 Token，或长度不在 32~512 之间（请设置 TRYON_TOKEN_FILE）"; fi
( umask 077; printf 'Authorization: Bearer %s\n' "$TOKEN" > "$T/auth.hdr"; printf '%s\n' "$TOKEN" > "$T/token.pat" )
unset TOKEN TRYON_TOKEN
# 一个格式正确但不存在的 Token，用于验证 AUTH_INVALID
( umask 077; printf 'Authorization: Bearer %s\n' "verify$(date +%s%N)ZqXw9Kp2Lm7Rt4Vb8Nc3Hd6Fg1Js5" > "$T/bad.hdr" )

PASS=0
FAIL=0
pass() { echo "PASS $1"; PASS=$((PASS+1)); }
fail() { echo "FAIL $1${2:+: $2}"; FAIL=$((FAIL+1)); }
check() { if [ "$2" = "$3" ]; then pass "$1"; else fail "$1" "实际 [$2]，期望 [$3]"; fi; }

BASE="https://$API_DOMAIN:$VERIFY_PORT"
[ "$VERIFY_PORT" = 443 ] && BASE="https://$API_DOMAIN"
CURL=(curl -sS --max-time 60 --proto '=https')
[ "$VERIFY_INSECURE" = 1 ] && { echo "警告：VERIFY_INSECURE=1，不校验证书（仅限测试）"; CURL+=(-k); }
[ -n "$VERIFY_CONNECT_IP" ] && CURL+=(--resolve "$API_DOMAIN:$VERIFY_PORT:$VERIFY_CONNECT_IP")

# req <方法> <路径> [curl 参数...]：响应头写入 $T/h，响应体写入 $T/b，输出状态码
req() {
  local method="$1" path="$2"; shift 2
  "${CURL[@]}" -X "$method" -D "$T/h" -o "$T/b" -w '%{http_code}' "$@" "$BASE$path" 2>"$T/err" || true
}
hdr() { tr -d '\r' < "$T/h" | grep -i "^$1:" | head -n1 | cut -d: -f2- | sed 's/^ *//'; }
json_field() { grep -o "\"$1\":\"[^\"]*\"" "$T/b" | head -n1 | cut -d'"' -f4; }
json_num() { grep -o "\"$1\":[0-9]*" "$T/b" | head -n1 | cut -d: -f2; }
# Nginx 对 /tryon 每 IP 20 次/分钟（突发 5）；非突发测试之间留出间隔，避免互相干扰
pace() { sleep 3.2; }

# 256×256 纯色 PNG（合法图片，用于让请求越过图片校验、到达 SSRF 校验）
PNG_B64='iVBORw0KGgoAAAANSUhEUgAAAQAAAAEACAIAAADTED8xAAACAElEQVR42u3TQQ0AAAjEsBOLJjQhizcaaFIFS5bpgrciAQYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABMIAKGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAbAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAGUAEDgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwAFwLiC0aXzI5w9gAAAAASUVORK5CYII='
ssrf_body() {
  printf '{"person_image":"data:image/png;base64,%s","garment_image":"data:image/png;base64,%s","model":"gemini","client_config":{"provider":"openai","base_url":"%s","api_key":"verify-dummy-not-a-key","model":"verify-dummy"}}' \
    "$PNG_B64" "$PNG_B64" "$1" > "$T/ssrf.json"
}

echo "== 验收目标 $BASE（本机模式: $LOCAL，重启测试: $RESTART）"

# ---------- 1. TLS 与跳转 ----------
code="$(req GET /hello)"
if [ "$code" = 200 ]; then pass "HTTPS 可访问（证书校验: $([ "$VERIFY_INSECURE" = 1 ] && echo 关闭 || echo 开启)）"; else fail "HTTPS 可访问" "状态码 [$code] $(head -c 200 "$T/err")"; fi
if [ "$VERIFY_HTTP_PORT" != 0 ]; then
  hc=(curl -sS --max-time 15 -o /dev/null -D "$T/h80" -w '%{http_code}')
  [ -n "$VERIFY_CONNECT_IP" ] && hc+=(--resolve "$API_DOMAIN:$VERIFY_HTTP_PORT:$VERIFY_CONNECT_IP")
  hport=""; [ "$VERIFY_HTTP_PORT" != 80 ] && hport=":$VERIFY_HTTP_PORT"
  code="$("${hc[@]}" "http://$API_DOMAIN$hport/verify-redirect" 2>/dev/null || true)"
  loc="$(tr -d '\r' < "$T/h80" 2>/dev/null | grep -i '^location:' | head -n1 | sed 's/^[^:]*: *//')"
  check "HTTP 301 跳转到 HTTPS" "$code ${loc%%://*}" "301 https"
fi

# ---------- 2. 安全响应头 ----------
code="$(req GET /hello)"
check "HSTS" "$(hdr strict-transport-security)" "max-age=31536000"
check "X-Content-Type-Options" "$(hdr x-content-type-options)" "nosniff"
check "X-Frame-Options" "$(hdr x-frame-options)" "DENY"
check "Referrer-Policy" "$(hdr referrer-policy)" "no-referrer"
check "Permissions-Policy" "$(hdr permissions-policy)" "camera=(), microphone=(), geolocation=(), payment=()"
check "Content-Security-Policy" "$(hdr content-security-policy)" "default-src 'none'; frame-ancestors 'none'"
srv="$(hdr server)"
if [[ "$srv" =~ [0-9] ]]; then fail "隐藏 Nginx 版本" "Server: $srv"; else pass "隐藏 Nginx 版本（Server: ${srv:-无}）"; fi

# ---------- 3. 探针访问控制 ----------
probe_expect=403
if [ "$LOCAL" = 1 ] || [[ "$VERIFY_CONNECT_IP" =~ ^(127\.|::1$) ]]; then probe_expect=200; fi
if [ "$probe_expect" = 403 ]; then
  check "公网访问 /healthz 被拒绝" "$(req GET /healthz)" "403"
  check "公网访问 /readyz 被拒绝" "$(req GET /readyz)" "403"
fi

# ---------- 4. 鉴权 ----------
code="$(req POST /tryon -H 'Content-Type: application/json' --data '{}')"
check "无 Token → 401 AUTH_REQUIRED" "$code $(json_field error_code)" "401 AUTH_REQUIRED"
pace
code="$(req POST /tryon -H @"$T/bad.hdr" -H 'Content-Type: application/json' --data '{}')"
check "错误 Token → 401 AUTH_INVALID" "$code $(json_field error_code)" "401 AUTH_INVALID"
pace
# 有效 Token + 空请求体：鉴权通过，被参数校验拦下（不调用上游、不扣额度）。若刚运行过本脚本被限流，则等待后重试
for _ in 1 2 3; do
  code="$(req POST /tryon -H @"$T/auth.hdr" -H 'Content-Type: application/json' --data '{}')"
  [ "$code" = 429 ] || break
  ra="$(hdr retry-after)"; sleep "$(( ${ra:-10} + 1 ))"
done
check "有效 Token → 400 参数校验（鉴权通过）" "$code $(json_num code)" "400 51"
if grep -q '"request_id":"[A-Za-z0-9_-]\{1,64\}"' "$T/b"; then pass "错误响应带 request_id"; else fail "错误响应带 request_id"; fi

# ---------- 5. CORS ----------
pace
code="$(req OPTIONS /tryon -H "Origin: $FRONTEND_ORIGIN" -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: authorization,content-type')"
check "CORS 预检：前端来源放行" "$code $(hdr access-control-allow-origin)" "200 $FRONTEND_ORIGIN"
pace
code="$(req OPTIONS /tryon -H 'Origin: https://verify-not-allowed.invalid' -H 'Access-Control-Request-Method: POST')"
check "CORS 预检：未授权来源 → 403 ORIGIN_NOT_ALLOWED" "$code $(json_field error_code)" "403 ORIGIN_NOT_ALLOWED"

# ---------- 6. SSRF 防护（个人模型 Base URL 指向内网/元数据地址） ----------
for target in https://127.0.0.1 https://169.254.169.254; do
  pace
  ssrf_body "$target"
  code="$(req POST /tryon -H @"$T/auth.hdr" -H 'Content-Type: application/json' --data-binary @"$T/ssrf.json")"
  if [ "$code" = 400 ] && grep -q '不允许指向内网或保留地址' "$T/b"; then pass "SSRF 拒绝 $target"; else fail "SSRF 拒绝 $target" "状态码 [$code] $(head -c 200 "$T/b")"; fi
done

# ---------- 7. 请求体上限 ----------
pace
head -c 32000000 /dev/zero > "$T/big.bin"
code="$(req POST /tryon -H 'Content-Type: application/json' --data-binary @"$T/big.bin")"
check "超大请求体 → 413 JSON" "$code $(json_field error_code)" "413 REQUEST_TOO_LARGE"
rm -f "$T/big.bin"

# ---------- 8. 限流 ----------
sleep 1
got429=""; ra=""
for _ in $(seq 1 12); do
  code="$(req POST /tryon -H @"$T/auth.hdr" -H 'Content-Type: application/json' --data '{}')"
  if [ "$code" = 429 ]; then got429="$(json_field error_code)"; ra="$(hdr retry-after)"; break; fi
done
if [ "$got429" = RATE_LIMITED ] && [[ "$ra" =~ ^[0-9]+$ ]]; then pass "连续请求触发 429 RATE_LIMITED（Retry-After: $ra）"; else fail "连续请求触发 429" "error_code [$got429] Retry-After [$ra]"; fi

# ---------- 9. 真实换装（可选，产生一次模型费用） ----------
data_url() {
  local mime
  case "${1,,}" in
    *.jpg|*.jpeg) mime=image/jpeg ;;
    *.png) mime=image/png ;;
    *.webp) mime=image/webp ;;
    *) die "仅支持 jpg/png/webp: $1" ;;
  esac
  printf 'data:%s;base64,' "$mime"
  base64 < "$1" | tr -d '\n'
}
if [ "$VERIFY_REAL_TRYON" = 1 ]; then
  echo "== 真实换装：模型 $VERIFY_MODEL（最长等待 200 秒）"
  sleep 20
  {
    printf '{"person_image":"'; data_url "$VERIFY_PERSON_IMAGE"
    printf '","garment_image":"'; data_url "$VERIFY_GARMENT_IMAGE"
    printf '","model":"%s","size":"1024x1536","lang":"zh"}' "$VERIFY_MODEL"
  } > "$T/real.json"
  code="$(req POST /tryon --max-time 200 -H @"$T/auth.hdr" -H 'Content-Type: application/json' --data-binary @"$T/real.json")"
  mime="$(json_field mime)"
  if [ "$code" = 200 ] && [[ "$mime" == image/* ]] && grep -q '"image":"[A-Za-z0-9+/]' "$T/b"; then
    pass "真实换装成功（$mime，$(wc -c < "$T/b" | tr -d ' ') 字节）"
  else
    fail "真实换装" "状态码 [$code] $(json_field error_code) $(head -c 200 "$T/b" | tr -d '\n')"
  fi
  rm -f "$T/real.json"
fi

# ---------- 10. 前端 ----------
if [ "$VERIFY_SKIP_FRONTEND" != 1 ]; then
  fc=(curl -sS --max-time 30 --proto '=https' -o "$T/fe" -w '%{http_code}')
  code="$("${fc[@]}" "$FRONTEND_URL" 2>/dev/null || true)"
  if [ "$code" = 200 ] && grep -q 'config.js' "$T/fe"; then pass "前端页面 HTTPS 200"; else fail "前端页面 HTTPS 200" "状态码 [$code]"; fi
  code="$("${fc[@]}" "${FRONTEND_URL%/}/config.js" 2>/dev/null || true)"
  check "前端 config.js 可访问" "$code" "200"
  if [ "$VERIFY_EXPECT_CONFIGJS" = 1 ]; then
    if grep -v '^[[:space:]]*//' "$T/fe" | grep -qF "https://$API_DOMAIN"; then pass "config.js 指向 https://$API_DOMAIN"; else fail "config.js 指向 https://$API_DOMAIN"; fi
  fi
fi

# ---------- 11. 服务器本机检查 ----------
if [ "$LOCAL" = 1 ]; then
  command -v docker >/dev/null 2>&1 || die "--local 需要在服务器上运行（未找到 docker）"
  [ -f deploy/docker-compose.prod.yml ] || die "--local 需要在仓库根目录运行"
  BK=costume-tryon-backend
  NG=costume-tryon-nginx
  LC=(curl -sS --max-time 15 --proto '=https' --resolve "$API_DOMAIN:$VERIFY_PORT:127.0.0.1")
  [ "$VERIFY_INSECURE" = 1 ] && LC+=(-k)
  probe() { "${LC[@]}" -o "$T/b" -w '%{http_code}' "https://$API_DOMAIN:$VERIFY_PORT$1" 2>/dev/null || true; }

  check "本机 /healthz" "$(probe /healthz) $(json_field status)" "200 ok"
  code="$(probe /readyz)"
  check "本机 /readyz" "$code $(json_field status)" "200 ready"
  for k in config lifecycle models state; do check "readyz.$k" "$(json_field "$k")" "ok"; done

  check "后端容器健康" "$(docker inspect -f '{{.State.Health.Status}}' "$BK" 2>/dev/null)" "healthy"
  check "Nginx 容器健康" "$(docker inspect -f '{{.State.Health.Status}}' "$NG" 2>/dev/null)" "healthy"
  check "后端未发布任何端口" "$(docker port "$BK" 2>/dev/null | wc -l | tr -d ' ')" "0"
  ports="$(docker port "$NG" 2>/dev/null | cut -d' ' -f1 | sort -u | tr '\n' ' ')"
  check "Nginx 只发布 80/443" "$ports" "443/tcp 80/tcp "
  check "后端重启策略" "$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "$BK")" "unless-stopped"
  check "Nginx 重启策略" "$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "$NG")" "unless-stopped"
  check "后端内存上限 3 GiB / 3 CPU" "$(docker inspect -f '{{.HostConfig.Memory}} {{.HostConfig.NanoCpus}}' "$BK")" "3221225472 3000000000"
  if docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$BK" | grep -qx 'GOMEMLIMIT=2GiB'; then pass "GOMEMLIMIT=2GiB"; else fail "GOMEMLIMIT=2GiB"; fi
  check "后端 IP 172.30.0.10" "$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$BK")" "172.30.0.10"
  check "Nginx IP 172.30.0.2" "$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$NG")" "172.30.0.2"
  check "日志轮转 20m×5" "$(docker inspect -f '{{index .HostConfig.LogConfig.Config "max-size"}}x{{index .HostConfig.LogConfig.Config "max-file"}}' "$BK")" "20mx5"
  check "生产配置已挂载（trustedProxies）" "$(docker exec "$BK" grep -c '172.30.0.2/32' /app/manifest/config/config.yaml 2>/dev/null)" "1"
  check "停机超时 190s" "$(docker exec "$BK" grep -cE '^[[:space:]]*timeoutSeconds:[[:space:]]*190' /app/manifest/config/config.yaml 2>/dev/null)" "1"
  check "stop_grace_period 200s" "$(docker inspect -f '{{.Config.StopTimeout}}' "$BK")" "200"

  docker logs "$BK" > "$T/logs" 2>&1
  docker logs "$NG" >> "$T/logs" 2>&1
  check "日志中不含 Token 明文" "$(grep -cF -f "$T/token.pat" "$T/logs")" "0"
  check "日志中不含 Bearer 头" "$(grep -ci 'bearer [A-Za-z0-9]' "$T/logs")" "0"
  check "日志中不含验收用假 Key" "$(grep -c 'verify-dummy-not-a-key' "$T/logs")" "0"
  check "无 trustedProxies 部署告警" "$(grep -c '部署告警' "$T/logs")" "0"
  rm -f "$T/logs"

  # 额度文件每次读写都会更新内容（/readyz 的 state 检查也会写入），因此用 inode 判断文件是否被重建，而不是比较哈希
  q1="$(docker exec "$BK" stat -c %i /app/data/quota.db 2>/dev/null)"
  if [ -n "$q1" ]; then pass "额度文件 /app/data/quota.db 存在"; else fail "额度文件 /app/data/quota.db 存在"; fi

  if [ "$RESTART" = 1 ]; then
    echo "== 优雅停机测试：停止后端（最多 200 秒）"
    since="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    t0=$(date +%s)
    docker stop -t 200 "$BK" >/dev/null
    el=$(( $(date +%s) - t0 ))
    check "后端退出码 0（非强杀）" "$(docker inspect -f '{{.State.ExitCode}}' "$BK")" "0"
    if [ "$el" -lt 200 ]; then pass "停机用时 ${el}s < 200s"; else fail "停机用时" "${el}s"; fi
    docker logs --since "$since" "$BK" > "$T/logs" 2>&1
    if grep -q '收到停机信号' "$T/logs"; then pass "停机日志：收到停机信号"; else fail "停机日志：收到停机信号"; fi
    if grep -q 'HTTP 服务已关闭' "$T/logs"; then pass "停机日志：HTTP 服务已关闭"; else fail "停机日志：HTTP 服务已关闭"; fi
    code="$(req GET /hello)"
    gw="$code $(json_field error_code)"
    case "$gw" in
      "502 GATEWAY_ERROR"|"504 GATEWAY_TIMEOUT") pass "后端停止时网关返回 JSON 错误（$gw）" ;;
      *) fail "后端停止时网关返回 JSON 错误" "实际 [$gw]" ;;
    esac
    docker start "$BK" >/dev/null
    st=""
    for _ in $(seq 1 30); do
      st="$(docker inspect -f '{{.State.Health.Status}}' "$BK")"
      [ "$st" = healthy ] && break
      sleep 3
    done
    check "后端重新启动并健康" "$st" "healthy"
    check "重启后 /readyz" "$(probe /readyz) $(json_field status)" "200 ready"
    q2="$(docker exec "$BK" stat -c %i /app/data/quota.db 2>/dev/null)"
    check "重启后沿用同一额度文件（卷未丢失、未重建）" "$q2" "$q1"
    check "无损坏备份文件" "$(docker exec "$BK" ls /app/data 2>/dev/null | grep -c corrupt)" "0"
  fi
fi

echo
echo "验收结果: pass=$PASS fail=$FAIL"
[ "$FAIL" -eq 0 ]
