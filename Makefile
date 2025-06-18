# 变量定义
IMAGE_NAME ?= iops-limit-service
IMAGE_TAG ?= latest
REGISTRY ?= your-registry
FULL_IMAGE_NAME = $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# Go 相关变量
BINARY_NAME = iops-limit-service
MAIN_FILE = main.go

# 默认目标
.PHONY: help
help: ## 显示帮助信息
	@echo "可用的目标:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## 构建 Go 二进制文件
	@echo "构建二进制文件..."
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o $(BINARY_NAME) $(MAIN_FILE)

.PHONY: build-local
build-local: ## 构建本地二进制文件
	@echo "构建本地二进制文件..."
	go build -o $(BINARY_NAME) $(MAIN_FILE)

.PHONY: docker-build
docker-build: ## 构建 Docker 镜像
	@echo "构建 Docker 镜像: $(FULL_IMAGE_NAME)"
	docker build -t $(FULL_IMAGE_NAME) .

.PHONY: docker-push
docker-push: ## 推送 Docker 镜像到仓库
	@echo "推送镜像到仓库: $(FULL_IMAGE_NAME)"
	docker push $(FULL_IMAGE_NAME)

.PHONY: docker-build-push
docker-build-push: docker-build docker-push ## 构建并推送 Docker 镜像

.PHONY: run
run: build-local ## 本地运行服务
	@echo "运行服务..."
	./$(BINARY_NAME)

.PHONY: run-docker
run-docker: ## 在 Docker 中运行服务
	@echo "在 Docker 中运行服务..."
	docker run --rm -it \
		--privileged \
		-v /sys/fs/cgroup:/sys/fs/cgroup \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /run/containerd/containerd.sock:/run/containerd/containerd.sock \
		-v /proc:/proc \
		-v /dev:/dev \
		-e CONTAINER_IOPS_LIMIT=500 \
		-e DATA_MOUNT=/data \
		-e CONTAINER_RUNTIME=auto \
		$(FULL_IMAGE_NAME)

.PHONY: deploy
deploy: ## 部署到 Kubernetes
	@echo "部署到 Kubernetes..."
	@sed 's|your-registry/iops-limit-service:latest|$(FULL_IMAGE_NAME)|g' k8s-daemonset.yaml | kubectl apply -f -

.PHONY: undeploy
undeploy: ## 从 Kubernetes 卸载
	@echo "从 Kubernetes 卸载..."
	kubectl delete -f k8s-daemonset.yaml

.PHONY: logs
logs: ## 查看服务日志
	@echo "查看服务日志..."
	kubectl logs -n kube-system -l app=iops-limit-service -f

.PHONY: status
status: ## 查看服务状态
	@echo "查看 DaemonSet 状态..."
	kubectl get daemonset -n kube-system iops-limit-service
	@echo ""
	@echo "查看 Pod 状态..."
	kubectl get pods -n kube-system -l app=iops-limit-service

.PHONY: test
test: ## 运行测试
	@echo "运行测试..."
	go test -v ./...

.PHONY: clean
clean: ## 清理构建文件
	@echo "清理构建文件..."
	rm -f $(BINARY_NAME)
	go clean

.PHONY: deps
deps: ## 下载依赖
	@echo "下载依赖..."
	go mod download
	go mod tidy

.PHONY: fmt
fmt: ## 格式化代码
	@echo "格式化代码..."
	go fmt ./...

.PHONY: lint
lint: ## 代码检查
	@echo "代码检查..."
	golangci-lint run

.PHONY: install-tools
install-tools: ## 安装开发工具
	@echo "安装开发工具..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.PHONY: all
all: deps fmt lint test build ## 执行完整的构建流程

# 开发相关目标
.PHONY: dev-setup
dev-setup: install-tools deps ## 设置开发环境

.PHONY: dev-run
dev-run: ## 开发模式运行
	@echo "开发模式运行..."
	@echo "设置环境变量..."
	@export CONTAINER_IOPS_LIMIT=500 && \
	export DATA_MOUNT=/data && \
	export CONTAINER_RUNTIME=docker && \
	go run $(MAIN_FILE)

# 测试相关目标
.PHONY: test-container
test-container: ## 创建测试容器
	@echo "创建测试容器..."
	docker run -d --name test-iops-container -v /data:/data alpine sleep 3600

.PHONY: test-iops
test-iops: ## 测试 IOPS 限制
	@echo "测试 IOPS 限制..."
	docker exec -it test-iops-container sh -c "apk add fio && fio --name=test-iops --directory=/data --rw=randrw --bs=4k --size=100M --numjobs=1 --iodepth=1 --runtime=60 --time_based --group_reporting --direct=1"

.PHONY: clean-test
clean-test: ## 清理测试容器
	@echo "清理测试容器..."
	docker rm -f test-iops-container 2>/dev/null || true

# 文档相关目标
.PHONY: docs
docs: ## 生成文档
	@echo "生成文档..."
	@echo "文档已生成: README.md"

.PHONY: version
version: ## 显示版本信息
	@echo "版本信息:"
	@echo "  Go 版本: $(shell go version)"
	@echo "  Git 提交: $(shell git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
	@echo "  构建时间: $(shell date)" 