# Build Stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# ✅ 这一步是关键：安装 git 和 ca-certificates 证书
RUN apk add --no-cache git ca-certificates

# 如果你在国内或者GitHub Action有时候慢，加上这个代理（可选，建议加上）
ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod ./
# 最好把 go.sum 加上，如果没有就算了
# COPY go.sum ./ 

RUN go mod download

COPY . .

# 编译
RUN CGO_ENABLED=0 GOOS=linux go build -o bot ./cmd/bot

# Run Stage
FROM alpine:latest

WORKDIR /root/
# 运行时镜像也要装证书，不然 HTTPS 请求会报错
RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/bot .

CMD ["./bot"]