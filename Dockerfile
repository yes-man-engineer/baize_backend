# ---- 构建 ----
FROM golang:1.22-alpine AS builder

WORKDIR /src

# 先只拷依赖清单，依赖没变时这一层能命中缓存。
# go.sum 必须一起拷，否则 go mod download 拿不到锁定版本。
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 静态编译，运行镜像里没有 libc。
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/baize ./cmd/api

# ---- 运行 ----
FROM alpine:3.20

# ca-certificates：要走 HTTPS 调大模型接口
# tzdata：日志和 verified_at 需要正确的本地时间
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app
ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=builder /out/baize /app/baize

USER app
EXPOSE 8080

CMD ["/app/baize"]
