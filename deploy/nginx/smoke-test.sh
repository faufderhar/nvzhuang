#!/usr/bin/env bash
# 本地验证 deploy/nginx 模板：nginx -t + 功能冒烟（XFF 覆盖、JSON 错误页、限流、安全头、跳转）。
# 只在本机回环地址上监听 18080/18081/18443，不访问外网。需要 nginx（1.25+）、python3、openssl、curl。
#   NGINX_BIN=/path/to/nginx deploy/nginx/smoke-test.sh
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
NG="${NGINX_BIN:-nginx}"
command -v "$NG" >/dev/null 2>&1 || { echo "未找到 nginx（可设置 NGINX_BIN），跳过"; exit 0; }
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; cd "$T" || exit 1
USER_LINE=""; [ "$(id -u)" -eq 0 ] && USER_LINE="user nobody $(id -gn nobody);"
mkdir -p logs conf acme
openssl req -x509 -newkey rsa:2048 -nodes -keyout key.pem -out cert.pem -days 1 -subj /CN=api.example.com 2>/dev/null
render() { HERE="$HERE" python3 - "$1" "$2" <<'PY'
import sys,re,os
here=os.environ['HERE']
src=open(here+'/templates/nvzhuang.conf.template').read()
env={'API_SERVER_NAME':'api.example.com','TLS_CERT_PATH':sys.argv[2]+'/cert.pem','TLS_KEY_PATH':sys.argv[2]+'/key.pem'}
for k,v in env.items(): src=src.replace('${'+k+'}',v)
assert '${' not in src, 'unrendered var'
if sys.argv[1]=='smoke':
    src=src.replace('listen 80;','listen 127.0.0.1:18081;').replace('listen 443 ssl;','listen 127.0.0.1:18443 ssl;')
    src=re.sub(r'\n\s*listen \[::\]:\d+( ssl)?;','',src)
    src=src.replace('172.30.0.10:8000','127.0.0.1:18080')
    src=src.replace('alias /etc/nginx/errors/;','alias '+here+'/errors/;').replace('root /var/www/acme;','root '+sys.argv[2]+'/acme;')
    src=src.replace('proxy_read_timeout 300s;','proxy_read_timeout 2s;')
open(sys.argv[2]+'/conf/site.conf','w').write(src)
PY
}
main() { cat > conf/nginx.conf <<C
$USER_LINE pid $T/nginx.pid; error_log $T/logs/error.log warn; worker_processes 1;
events { worker_connections 256; }
http { default_type application/octet-stream; access_log off;
  client_body_temp_path $T/tmp_body; proxy_temp_path $T/tmp_proxy; fastcgi_temp_path $T/t1; uwsgi_temp_path $T/t2; scgi_temp_path $T/t3;
  include $T/conf/site.conf; }
C
}
main
if [ "$(id -u)" -eq 0 ]; then
  render prod "$T" && "$NG" -p "$T" -c "$T/conf/nginx.conf" -t || { echo "FAIL 生产模板 nginx -t"; exit 1; }
else
  echo "非 root：跳过生产模板（80/443 端口）的 nginx -t，只测试冒烟版本"
fi
render smoke "$T" && "$NG" -p "$T" -c "$T/conf/nginx.conf" -t || { echo "FAIL 冒烟模板 nginx -t"; exit 1; }
python3 "$HERE/smoke_mock_upstream.py" 18080 & MOCK=$!
"$NG" -p "$T" -c "$T/conf/nginx.conf" 2>/dev/null; sleep 1
C="curl -sk --resolve api.example.com:18443:127.0.0.1"
pass=0; fail=0; check() { if [ "$2" = "$3" ]; then echo "PASS $1"; pass=$((pass+1)); else echo "FAIL $1: got [$2] want [$3]"; fail=$((fail+1)); fi; }
r=$($C -H 'X-Forwarded-For: 6.6.6.6, 7.7.7.7' -H 'X-Real-IP: 8.8.8.8' https://api.example.com:18443/hello)
check "XFF 被覆盖为对端地址" "$(echo "$r" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["xff"],d["xri"],d["proto"])')" "127.0.0.1 127.0.0.1 https"
h=$($C -D - -o /dev/null https://api.example.com:18443/hello | tr -d '\r')
check "HSTS" "$(echo "$h" | grep -ci '^strict-transport-security')" "1"
check "nosniff" "$(echo "$h" | grep -ci '^x-content-type-options: nosniff')" "1"
check "Permissions-Policy" "$(echo "$h" | grep -ci '^permissions-policy:')" "1"
check "隐藏版本" "$(echo "$h" | grep -i '^server:' | grep -c '[0-9]')" "0"
check "HTTP 跳转 HTTPS" "$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: api.example.com' http://127.0.0.1:18081/x)" "301"
head -c 32000000 /dev/zero > big.bin
r=$($C -o body413 -w '%{http_code} %{content_type}' -X POST --data-binary @big.bin https://api.example.com:18443/tryon)
check "413 JSON" "$r $(python3 -c 'import json;print(json.load(open("body413"))["data"]["error_code"])')" "413 application/json REQUEST_TOO_LARGE"
codes=""; for _ in $(seq 1 10); do codes="$codes $($C -o /dev/null -w '%{http_code}' -X POST -d '{}' https://api.example.com:18443/tryon)"; done
check "tryon 突发限流出现 429" "$(echo "$codes" | grep -c 429)" "1"
r=$($C -D - -o body429 -X POST -d '{}' https://api.example.com:18443/tryon | tr -d '\r')
check "网关 429 JSON + Retry-After" "$(echo "$r" | grep -ci '^retry-after: 10') $(python3 -c 'import json;print(json.load(open("body429"))["data"]["error_code"])')" "1 RATE_LIMITED"
check "网关错误页保留安全头" "$(echo "$r" | grep -ciE '^(strict-transport-security|x-content-type-options|permissions-policy|content-security-policy):')" "4"
r=$($C -D - -o b2 https://api.example.com:18443/backend429 | tr -d '\r')
check "后端 JSON 429 原样透传" "$(python3 -c 'import json;print(json.load(open("b2"))["data"]["error_code"])') $(echo "$r" | grep -i '^retry-after' | awk '{print $2}')" "CONCURRENCY_LIMITED 5"
r=$($C -o b504 -w '%{http_code}' https://api.example.com:18443/slow)
check "504 JSON（测试用 2s 读超时）" "$r $(python3 -c 'import json;print(json.load(open("b504"))["data"]["error_code"])')" "504 GATEWAY_TIMEOUT"
check "探针本机可访问" "$($C -o /dev/null -w '%{http_code}' https://api.example.com:18443/healthz)" "200"
kill $MOCK; sleep 0.5
r=$($C -o b502 -w '%{http_code}' https://api.example.com:18443/hello)
check "502 JSON（上游不可用）" "$r $(python3 -c 'import json;print(json.load(open("b502"))["data"]["error_code"])')" "502 GATEWAY_ERROR"
"$NG" -p "$T" -c "$T/conf/nginx.conf" -s stop 2>/dev/null
echo "nginx smoke: pass=$pass fail=$fail"; [ $fail -eq 0 ]
