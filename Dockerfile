# syntax=docker/dockerfile:1.4

# 构建阶段
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=dev
ARG BUILD_TIME=unknown
ARG BIN_NAME=kubediskguard

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
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-X 'main.Version=$VERSION' -X 'main.GitCommit=$GIT_COMMIT' -X 'main.BuildTime=$BUILD_TIME'" -o $BIN_NAME main.go

# 运行阶段
FROM alpine:3.18
WORKDIR /app
ARG BIN_NAME=kubediskguard

# 安装必要的系统工具
RUN apk add --no-cache ca-certificates tzdata util-linux

# 从构建阶段复制二进制文件
COPY --from=builder /app/$BIN_NAME /app/kubediskguard
COPY --from=builder /app/README.md .

# 暴露端口（如果需要的话）
# EXPOSE 8080

# 运行应用
ENTRYPOINT ["/app/kubediskguard"] 