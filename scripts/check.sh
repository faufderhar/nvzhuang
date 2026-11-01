#!/usr/bin/env bash
# 本地质量门禁：对应 AGENTS.md §3 提交前检查清单，全部在本机执行，不依赖任何云端 CI。
#
# 用法：
#   scripts/check.sh              常规检查（提交前必跑）
#   scripts/check.sh --release    发布前检查：在常规检查之上追加 -count=5、镜像构建与镜像漏洞扫描
#   scripts/check.sh --self-test  只验证脚本自身的敏感串扫描规则是否生效
#   scripts/check.sh --staged     快速检查：只扫描暂存区内容中的敏感串与禁止文件（pre-commit 钩子默认使用）
#   scripts/check.sh --install-hook  启用 .githooks/pre-commit（git config core.hooksPath .githooks）
#
# 任何一步失败都会以非零状态退出；命令输出不经过管道过滤，避免吞掉失败信息。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# 与 AGENTS.md §3 步骤 3 保持一致的高危密钥正则：
#   OpenAI 等 sk-（含 sk-proj- / sk-ant- 等带连字符的新格式）、Google AIza、GitHub ghp_/gho_/ghu_/ghs_/ghr_ 与 github_pat_、
#   Slack xox?-、AWS AKIA、PEM 私钥头
SECRET_RE='(sk-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{20,}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----)'
# 不允许进入版本库的文件（日志、真实配置、编译产物、环境变量文件）
FORBIDDEN_RE='(\.log$|(^|/)config\.yaml$|(^|/)\.env$|\.exe$|(^|/)backend/main$)'
# 例外：GoFrame CLI 的开发工具配置（不含任何凭据）
ALLOWED_RE='^backend/hack/config\.yaml$'

MODE="normal"
case "${1:-}" in
  "") ;;
  --release) MODE="release" ;;
  --self-test) MODE="self-test" ;;
  --staged) MODE="staged" ;;
  --install-hook) MODE="install-hook" ;;
  -h|--help) sed -n '2,11p' "$0"; exit 0 ;;
  *) echo "未知参数: $1（可用 --release / --self-test / --staged / --install-hook）" >&2; exit 2 ;;
esac

step() { printf '\n== %s\n' "$*"; }
fail() { printf '\n[失败] %s\n' "$*" >&2; exit 1; }

# 列出需要扫描的文件：已跟踪 + 未跟踪但未被忽略的文件
candidate_files() {
  git ls-files -z --cached --others --exclude-standard
}

scan_secrets() {
  local hits
  hits="$(candidate_files | xargs -0 -r grep -nIHE "$SECRET_RE" -- 2>/dev/null || true)"
  if [[ -n "$hits" ]]; then
    printf '%s\n' "$hits" >&2
    return 1
  fi
  return 0
}

# scan_staged：只扫描暂存区（即将提交）的文件内容，比全量扫描快，供 pre-commit 钩子使用
scan_staged() {
  local f hits="" bad=""
  while IFS= read -r -d '' f; do
    if [[ "$f" =~ $FORBIDDEN_RE ]] && ! [[ "$f" =~ $ALLOWED_RE ]]; then bad+="$f"$'\n'; fi
    if git show ":$f" 2>/dev/null | grep -qIE "$SECRET_RE"; then hits+="$f"$'\n'; fi
  done < <(git diff --cached --name-only --diff-filter=ACMR -z)
  [[ -z "$bad" ]] || { printf '%s' "$bad" >&2; fail "暂存区包含不应提交的文件"; }
  [[ -z "$hits" ]] || { printf '%s' "$hits" >&2; fail "暂存区文件中发现疑似真实密钥，请移除后再提交"; }
  echo "暂存区检查通过（完整检查请运行 scripts/check.sh）"
}

self_test() {
  step "自检：敏感串正则"
  local tmp ex
  tmp="$(mktemp)"
  # 运行时拼接示例串，避免把形似密钥的字符串写进仓库
  for ex in "sk-$(printf 'a%.0s' {1..24})" "sk-proj-$(printf 'Ab1_-%.0s' {1..6})" "AIza$(printf 'b%.0s' {1..24})" \
    "ghp_$(printf 'c%.0s' {1..36})" "github_pat_$(printf 'd%.0s' {1..40})" "xoxb-$(printf '1%.0s' {1..12})" \
    "AKIA$(printf 'E%.0s' {1..16})" "-----BEGIN RSA PRIVATE"" KEY-----"; do
    printf 'key=%s\n' "$ex" >"$tmp"
    grep -qE "$SECRET_RE" "$tmp" || { rm -f "$tmp"; fail "正则未命中示例: ${ex:0:8}…"; }
  done
  printf 'key=%s\n' "sk-short" >"$tmp"
  if grep -qE "$SECRET_RE" "$tmp"; then rm -f "$tmp"; fail "正则误报短串"; fi
  rm -f "$tmp"
  printf 'backend/app.log\nbackend/manifest/config/config.yaml\n.env\nbackend/main\n' | while read -r f; do
    [[ "$f" =~ $FORBIDDEN_RE ]] || fail "禁止文件规则未命中: $f"
  done
  printf 'backend/manifest/config/config.example.yaml\n.env.example\nbackend/main.go\n' | while read -r f; do
    if [[ "$f" =~ $FORBIDDEN_RE ]]; then fail "禁止文件规则误报: $f"; fi
  done
  echo "自检通过"
}

find_govulncheck() {
  if command -v govulncheck >/dev/null 2>&1; then
    command -v govulncheck
    return
  fi
  local gp
  gp="$(go env GOPATH)"
  if [[ -x "$gp/bin/govulncheck" ]]; then
    echo "$gp/bin/govulncheck"
    return
  fi
  return 1
}

if [[ "$MODE" == "self-test" ]]; then
  self_test
  exit 0
fi
if [[ "$MODE" == "install-hook" ]]; then
  git config core.hooksPath .githooks
  echo "已启用 .githooks/pre-commit：每次提交前快速扫描暂存区；设置 NVZ_HOOK_FULL=1 时运行完整检查"
  exit 0
fi
if [[ "$MODE" == "staged" ]]; then
  scan_staged
  exit 0
fi

self_test

step "步骤 1：禁止进入版本库的文件"
bad="$(git ls-files --cached --others --exclude-standard | grep -E "$FORBIDDEN_RE" | grep -vE "$ALLOWED_RE" || true)"
[[ -z "$bad" ]] || { printf '%s\n' "$bad" >&2; fail "发现不应提交的文件"; }
echo "通过"

step "步骤 3：敏感串扫描 $SECRET_RE"
scan_secrets || fail "发现疑似真实密钥，请移除后再提交"
echo "通过"

step "步骤 4：后端 go vet / test / race / govulncheck"
GOVULN="$(find_govulncheck)" || fail "未找到 govulncheck，请先执行: go install golang.org/x/vuln/cmd/govulncheck@latest"
(
  cd backend
  go vet ./...
  go test ./...
  go test -race ./...
  "$GOVULN" ./...
  if [[ "$MODE" == "release" ]]; then
    go test -count=5 ./...
  fi
)

step "步骤 4：前端测试（tests/*.mjs）"
shopt -s nullglob
fe_tests=(tests/*.mjs)
[[ ${#fe_tests[@]} -gt 0 ]] || fail "未找到前端测试"
for t in "${fe_tests[@]}"; do
  echo "-- node $t"
  node "$t"
done

step "Nginx 模板冒烟（可选）"
if [[ -n "${NGINX_BIN:-}" ]] || command -v nginx >/dev/null 2>&1; then
  deploy/nginx/smoke-test.sh
else
  echo "（未找到 nginx，跳过；可设置 NGINX_BIN 指向 nginx 可执行文件）"
fi

step "Shell 脚本语法检查"
mapfile -t sh_files < <(git ls-files --cached --others --exclude-standard -- '*.sh' '.githooks/*')
for f in "${sh_files[@]}"; do
  bash -n "$f"
done
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -S warning "${sh_files[@]}"
else
  echo "（未安装 shellcheck，跳过静态检查）"
fi

if [[ "$MODE" == "release" ]]; then
  step "发布检查：构建镜像"
  command -v docker >/dev/null 2>&1 || fail "发布检查需要 docker"
  docker build -f backend/manifest/docker/Dockerfile -t nvzhuang-backend:check backend
  step "发布检查：镜像漏洞扫描"
  if command -v trivy >/dev/null 2>&1; then
    trivy image --exit-code 1 --severity HIGH,CRITICAL nvzhuang-backend:check
  elif command -v grype >/dev/null 2>&1; then
    grype --fail-on high nvzhuang-backend:check
  else
    fail "发布检查需要 trivy 或 grype"
  fi
  if [[ -x scripts/e2e.sh ]]; then
    step "发布检查：端到端测试"
    scripts/e2e.sh
  else
    echo "（scripts/e2e.sh 尚未提供，跳过端到端测试；见优化方案阶段 2 O-02）"
  fi
fi

printf '\n全部检查通过（模式: %s）\n' "$MODE"
