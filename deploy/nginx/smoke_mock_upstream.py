# deploy/nginx/smoke-test.sh 使用的本地模拟上游：回显转发头，/slow 延迟 3 秒，/backend429 模拟后端 JSON 429，OPTIONS 返回 204 + X-Mock 用于确认预检被转发
import http.server, json, time, sys
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self,*a): pass
    def _send(self, code, obj, extra=None):
        b=json.dumps(obj).encode()
        self.send_response(code); self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(b)))
        for k,v in (extra or {}).items(): self.send_header(k,v)
        self.end_headers(); self.wfile.write(b)
    def do_GET(self):
        if self.path.startswith('/slow'): time.sleep(3)
        if self.path.startswith('/backend429'):
            return self._send(429, {"code":42903,"message":"backend","data":{"error_code":"CONCURRENCY_LIMITED"}}, {"Retry-After":"5"})
        self._send(200, {"path":self.path,"xff":self.headers.get('X-Forwarded-For'),"xri":self.headers.get('X-Real-IP'),"proto":self.headers.get('X-Forwarded-Proto')})
    def do_OPTIONS(self):
        self.send_response(204); self.send_header('X-Mock','options'); self.send_header('Content-Length','0'); self.end_headers()
    def do_POST(self):
        n=int(self.headers.get('Content-Length') or 0); self.rfile.read(n)
        self._send(200, {"ok":True,"len":n})
http.server.ThreadingHTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()
