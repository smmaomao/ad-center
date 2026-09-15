# syntax=docker/dockerfile:1
# 多阶段构建：构建期用 golang:1.26，运行期用精简 debian。
# 注意：本项目主包在 ./cmd/server（仓库根目录没有 main 包），
# 因此 go build 必须指向 ./cmd/server，否则报 "no Go files in /usr/src/app"。

FROM golang:1.26-bookworm AS builder
WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
# 主包路径 = ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /run-app ./cmd/server

FROM debian:bookworm-slim
# ca-certificates：DB 走 TLS(sslmode=require) 与对外 HTTPS 回调用
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /run-app /app/run-app
# fly 会把 PORT 环境变量设为 internal_port 的值，应用监听 :PORT
EXPOSE 8080
CMD ["/app/run-app"]
