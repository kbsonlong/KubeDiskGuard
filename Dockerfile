# 构建阶段
FROM golang:1.21-alpine AS builder

# 安装必要的系统工具
RUN apk add --no-cache git

# 设置工作目录
WORKDIR /app

# 复制go mod文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o iops-limit-service .

# 运行阶段
FROM alpine:latest

# 安装必要的系统工具
RUN apk add --no-cache ca-certificates tzdata

# 创建非root用户
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/iops-limit-service .

# 更改文件所有者
RUN chown appuser:appgroup /app/iops-limit-service

# 切换到非root用户
USER appuser

# 暴露端口（如果需要的话）
# EXPOSE 8080

# 运行应用
CMD ["./iops-limit-service"] 