# 构建阶段
FROM golang:1.26-alpine AS builder

WORKDIR /app
# 先复制依赖文件，利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并编译（.dockerignore 已排除 config.yaml、.env、日志等本地文件，避免把密钥打进镜像）
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o main .

# 运行阶段
FROM alpine:3.22

# 安装 ca-certificates 确保能够正常调用外部 HTTPS API（OpenAI / 火山方舟 / Google 等）
RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app \
    && mkdir -p /app/data && chown app:app /app/data && chmod 700 /app/data

WORKDIR /app
COPY --from=builder /app/main .
# 镜像内只放示例配置；真实 config.yaml 请在运行时以只读卷挂载到 /app/manifest/config/config.yaml，
# 未挂载时程序会自动回退加载 config.example.yaml。
COPY manifest/config/config.example.yaml manifest/config/
COPY workflows workflows/

# 每日额度持久化目录（security.stateDir: "/app/data" 时使用），需以卷挂载，镜像根文件系统可保持只读
VOLUME ["/app/data"]
USER app:app
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8000/healthz || exit 1
# 收到 SIGTERM 后优雅停机（见 config.example.yaml 的 shutdown 段），编排侧 stop timeout 需 ≥ 200 秒
STOPSIGNAL SIGTERM
CMD ["./main"]
