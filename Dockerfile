# Dockerfile for html-site
# 多阶段构建：编译阶段用 golang 镜像，运行阶段用 alpine（含 CA 证书，便于 client 模式访问 HTTPS）。
# 产物是单个二进制，运行时只需挂载 data 目录做持久化。

# ---- 编译阶段 ----
FROM golang:1.25-alpine AS builder

# CGO 默认关闭（modernc.org/sqlite 是纯 Go，免 CGO）
ENV CGO_ENABLED=0 GOOS=linux

WORKDIR /src

# 先拷依赖文件，利用层缓存
COPY go.mod go.sum ./
RUN go mod download

# 拷源码并编译
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/html-site ./cmd/html-site

# ---- 运行阶段 ----
FROM alpine:3.20

# 安装 ca-certificates（client 模式访问 HTTPS 需要）+ tzdata（时区）
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

# 拷贝二进制
COPY --from=builder /out/html-site /app/html-site

# 数据目录（挂载卷做持久化）
RUN mkdir -p /app/data && chown -R app:app /app
VOLUME ["/app/data"]

USER app

EXPOSE 8080

# 默认监听 :8080，数据目录 /app/data；均可通过环境变量覆盖
ENV HTML_SITE_ADDR=:8080 \
    HTML_SITE_DATA=/app/data

ENTRYPOINT ["/app/html-site"]
CMD ["serve"]
