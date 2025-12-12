# 使用最新的 Go 版本，避免老版本的一些 bug
FROM golang:1.22-alpine AS builder

WORKDIR /app

# 安装必要的工具
RUN apk add --no-cache git tree

# 设置环境
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOPROXY=https://goproxy.cn,direct

# 1. 复制依赖文件
COPY go.mod ./
# 只要没报错，先生成 go.sum
RUN go mod tidy

# 2. 复制所有源码
COPY . .

# 🔥 调试核心：把当前目录下的所有文件结构打印出来
# 这一步能让你在 build 日志里看到到底拷进去了些什么
RUN echo "============ 📂 FILE STRUCTURE ============" && \
    tree . && \
    echo "==========================================="

# 🔥 调试核心：先尝试编译一下 internal 包，看看是哪个包坏了
RUN echo "🛠️ Checking internal packages..." && \
    go build -v ./internal/... || echo "❌ Internal build failed"

# 3. 正式编译主程序 (加上 -x 参数显示详细执行过程)
RUN echo "🚀 Building Main..." && \
    go build -v -x -o bot ./cmd/bot

# Run Stage
FROM alpine:latest
WORKDIR /root/
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /app/bot .
CMD ["./bot"]