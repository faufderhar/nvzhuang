#!/usr/bin/env bash
# 检查仓库中固定的 Docker 基础镜像摘要是否与 Docker Hub 上同一 tag 的当前摘要一致（只读，不修改文件）。
# 需要访问 auth.docker.io 与 registry-1.docker.io（HTTPS）。用法：scripts/image-digests.sh
# 摘要不一致说明上游已发布新构建（通常含安全补丁）：确认后把文件中的 @sha256:... 替换为新值，
# 重新构建并验收（本地 scripts/check.sh + 服务器 deploy/verify.sh）后再提交。
set -euo pipefail
cd "$(dirname "$0")/.."
FILES=(deploy/docker-compose.prod.yml backend/manifest/docker/Dockerfile)
ACCEPT='application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json'
status=0
while read -r ref; do
  name="${ref%@*}"; pinned="${ref#*@}"; repo="${name%:*}"; tag="${name##*:}"
  [[ "$repo" == */* ]] || repo="library/$repo"
  token="$(curl -fsS --proto '=https' --max-time 20 "https://auth.docker.io/token?service=registry.docker.io&scope=repository:$repo:pull" \
    | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')"
  current="$(curl -fsS --proto '=https' --max-time 20 -I -H "Authorization: Bearer $token" -H "Accept: $ACCEPT" \
    "https://registry-1.docker.io/v2/$repo/manifests/$tag" | tr -d '\r' | awk 'tolower($1)=="docker-content-digest:"{print $2}')"
  if [ "$current" = "$pinned" ]; then echo "OK      $name $pinned"
  else echo "CHANGED $name 固定 $pinned → 当前 ${current:-<查询失败>}"; status=1; fi
done < <(grep -hoE '[a-z0-9./_-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}' "${FILES[@]}" | sort -u)
exit $status
