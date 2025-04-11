# Dockerfile

# --- Stage 1: Build ---
# 使用官方 Go 镜像作为构建环境。选择一个与你 go.mod 文件中指定的 Go 版本匹配或兼容的版本。
# Alpine 版本通常更小。
FROM golang:1.24.1 AS builder

# 设置工作目录
WORKDIR /app

# 安装必要的构建工具 (如果需要 CGO，可能需要 gcc/musl-dev 等)
# 对于这个项目，目前看起来不需要额外的构建依赖
# RUN apk add --no-cache gcc musl-dev

# 复制 Go 模块文件
COPY go.mod go.sum ./

# 下载依赖项。利用 Docker 层缓存，只有在 go.mod 或 go.sum 改变时才重新下载。
RUN go mod download

# 复制所有源代码到工作目录
COPY . .

# 编译应用程序
# -ldflags="-w -s" 用于移除调试信息和符号表，减小最终二进制文件体积。
# -o /server 将编译后的可执行文件输出到 /server (在构建阶段的根目录下)
RUN go build -ldflags="-w -s" -o /server ./cmd/server/main.go

# --- Stage 2: Runtime ---
# 使用一个轻量级的 Alpine Linux 作为最终运行环境
FROM alpine:latest

# 安装必要的运行时依赖
# ca-certificates: 如果你的应用需要进行 HTTPS 调用 (即使是内部服务也可能是)
# tzdata: 如果你的应用需要处理时区信息 (例如 time.LoadLocation)
RUN apk add --no-cache ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 从构建阶段复制编译好的二进制文件到当前阶段的工作目录
COPY --from=builder /server /app/server

# 复制扩展知识 YAML 文件目录到镜像中
# 确保你的 extensions_knowledge 目录与 Dockerfile 在同一层级或能被 COPY 指令访问
COPY extensions_knowledge ./extensions_knowledge

# 复制 .env 配置文件到镜像中
# 注意：对于生产环境，更推荐使用 Docker secrets 或在容器运行时注入环境变量，
# 而不是直接将 .env 文件打包进镜像，特别是当 .env 包含敏感信息时。
# 但为了本地开发和测试方便，这里包含它。
COPY .env .env

# (可选，但推荐) 创建一个非 root 用户来运行应用
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
# 切换到非 root 用户
USER appuser

# 声明服务器将监听的端口 (与你的 config.ServerAddr 匹配)
# 这主要用于文档目的，实际端口映射在 docker run 或 docker-compose 中完成。
EXPOSE 8181

# 设置容器启动时运行的命令
# 执行我们编译好的二进制文件
CMD ["./server"]