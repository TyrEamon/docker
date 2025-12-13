# Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# 安装 git 和证书
RUN apk add --no-cache git ca-certificates

# 设置代理
ENV GOPROXY=https://goproxy.cn,direct

# 1. 这一步直接把所有源代码（包括 go.mod）全部拷进去！
# 不要分步拷了，分步拷虽然能利用缓存，但在这种初次构建且没有 go.sum 的情况下会出问题。
COPY . .

# 强制升级库到最新版
RUN go get -u github.com/go-telegram/bot  # <--- ✅ 加上这行

# 2. 现在有了源代码，go mod tidy 才能正确分析出你需要哪些包
RUN touch go.sum
RUN go mod tidy

# 3. 编译
RUN CGO_ENABLED=0 GOOS=linux go build -o bot ./cmd/bot

# Run Stage
FROM alpine:latest

WORKDIR /root/
RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/bot .

CMD ["./bot"]
