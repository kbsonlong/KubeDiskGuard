#!/bin/bash

# Kubernetes NVMe 磁盘 IOPS 限速服务部署脚本

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 默认配置
IMAGE_NAME="iops-limit-service"
IMAGE_TAG="latest"
REGISTRY="your-registry"
NAMESPACE="kube-system"
CONFIG_FILE="k8s-daemonset.yaml"

# 帮助信息
show_help() {
    echo "Kubernetes NVMe 磁盘 IOPS 限速服务部署脚本"
    echo ""
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  -h, --help              显示帮助信息"
    echo "  -r, --registry REGISTRY 设置镜像仓库地址"
    echo "  -t, --tag TAG           设置镜像标签"
    echo "  -n, --namespace NS      设置 Kubernetes 命名空间"
    echo "  -f, --file FILE         设置配置文件路径"
    echo "  --build                 构建 Docker 镜像"
    echo "  --push                  推送 Docker 镜像"
    echo "  --deploy                部署到 Kubernetes"
    echo "  --undeploy              从 Kubernetes 卸载"
    echo "  --status                查看部署状态"
    echo "  --logs                  查看服务日志"
    echo "  --test                  运行测试"
    echo ""
    echo "示例:"
    echo "  $0 --build --push --deploy"
    echo "  $0 --registry my-registry.com --tag v1.0.0 --deploy"
    echo "  $0 --status"
}

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查依赖
check_dependencies() {
    log_info "检查依赖..."
    
    # 检查 Docker
    if ! command -v docker &> /dev/null; then
        log_error "Docker 未安装或不在 PATH 中"
        exit 1
    fi
    
    # 检查 kubectl
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl 未安装或不在 PATH 中"
        exit 1
    fi
    
    # 检查 Go
    if ! command -v go &> /dev/null; then
        log_error "Go 未安装或不在 PATH 中"
        exit 1
    fi
    
    log_success "所有依赖检查通过"
}

# 构建镜像
build_image() {
    log_info "构建 Docker 镜像..."
    
    FULL_IMAGE_NAME="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"
    
    if docker build -t "$FULL_IMAGE_NAME" .; then
        log_success "镜像构建成功: $FULL_IMAGE_NAME"
    else
        log_error "镜像构建失败"
        exit 1
    fi
}

# 推送镜像
push_image() {
    log_info "推送 Docker 镜像..."
    
    FULL_IMAGE_NAME="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"
    
    if docker push "$FULL_IMAGE_NAME"; then
        log_success "镜像推送成功: $FULL_IMAGE_NAME"
    else
        log_error "镜像推送失败"
        exit 1
    fi
}

# 部署到 Kubernetes
deploy_to_k8s() {
    log_info "部署到 Kubernetes..."
    
    FULL_IMAGE_NAME="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"
    
    # 检查配置文件是否存在
    if [ ! -f "$CONFIG_FILE" ]; then
        log_error "配置文件不存在: $CONFIG_FILE"
        exit 1
    fi
    
    # 替换镜像地址并部署
    if sed "s|your-registry/iops-limit-service:latest|$FULL_IMAGE_NAME|g" "$CONFIG_FILE" | kubectl apply -f -; then
        log_success "部署成功"
    else
        log_error "部署失败"
        exit 1
    fi
    
    # 等待部署完成
    log_info "等待部署完成..."
    kubectl rollout status daemonset/iops-limit-service -n "$NAMESPACE" --timeout=300s
}

# 从 Kubernetes 卸载
undeploy_from_k8s() {
    log_info "从 Kubernetes 卸载..."
    
    if kubectl delete -f "$CONFIG_FILE" --ignore-not-found=true; then
        log_success "卸载成功"
    else
        log_error "卸载失败"
        exit 1
    fi
}

# 查看部署状态
show_status() {
    log_info "查看部署状态..."
    
    echo ""
    echo "=== DaemonSet 状态 ==="
    kubectl get daemonset -n "$NAMESPACE" iops-limit-service
    
    echo ""
    echo "=== Pod 状态 ==="
    kubectl get pods -n "$NAMESPACE" -l app=iops-limit-service
    
    echo ""
    echo "=== 服务日志 ==="
    kubectl logs -n "$NAMESPACE" -l app=iops-limit-service --tail=10
}

# 查看服务日志
show_logs() {
    log_info "查看服务日志..."
    kubectl logs -n "$NAMESPACE" -l app=iops-limit-service -f
}

# 运行测试
run_tests() {
    log_info "运行测试..."
    
    if go test -v ./...; then
        log_success "测试通过"
    else
        log_error "测试失败"
        exit 1
    fi
}

# 验证部署
verify_deployment() {
    log_info "验证部署..."
    
    # 检查 DaemonSet 是否就绪
    if kubectl get daemonset -n "$NAMESPACE" iops-limit-service -o jsonpath='{.status.numberReady}' | grep -q "$(kubectl get daemonset -n "$NAMESPACE" iops-limit-service -o jsonpath='{.status.desiredNumberScheduled}')"; then
        log_success "DaemonSet 部署验证通过"
    else
        log_error "DaemonSet 部署验证失败"
        exit 1
    fi
    
    # 检查 Pod 是否运行
    if kubectl get pods -n "$NAMESPACE" -l app=iops-limit-service --field-selector=status.phase=Running | grep -q "iops-limit-service"; then
        log_success "Pod 运行状态验证通过"
    else
        log_error "Pod 运行状态验证失败"
        exit 1
    fi
}

# 主函数
main() {
    # 解析命令行参数
    BUILD=false
    PUSH=false
    DEPLOY=false
    UNDEPLOY=false
    STATUS=false
    LOGS=false
    TEST=false
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -r|--registry)
                REGISTRY="$2"
                shift 2
                ;;
            -t|--tag)
                IMAGE_TAG="$2"
                shift 2
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            -f|--file)
                CONFIG_FILE="$2"
                shift 2
                ;;
            --build)
                BUILD=true
                shift
                ;;
            --push)
                PUSH=true
                shift
                ;;
            --deploy)
                DEPLOY=true
                shift
                ;;
            --undeploy)
                UNDEPLOY=true
                shift
                ;;
            --status)
                STATUS=true
                shift
                ;;
            --logs)
                LOGS=true
                shift
                ;;
            --test)
                TEST=true
                shift
                ;;
            *)
                log_error "未知选项: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 检查依赖
    check_dependencies
    
    # 执行操作
    if [ "$TEST" = true ]; then
        run_tests
    fi
    
    if [ "$BUILD" = true ]; then
        build_image
    fi
    
    if [ "$PUSH" = true ]; then
        push_image
    fi
    
    if [ "$DEPLOY" = true ]; then
        deploy_to_k8s
        verify_deployment
    fi
    
    if [ "$UNDEPLOY" = true ]; then
        undeploy_from_k8s
    fi
    
    if [ "$STATUS" = true ]; then
        show_status
    fi
    
    if [ "$LOGS" = true ]; then
        show_logs
    fi
    
    # 如果没有指定任何操作，显示帮助
    if [ "$BUILD" = false ] && [ "$PUSH" = false ] && [ "$DEPLOY" = false ] && [ "$UNDEPLOY" = false ] && [ "$STATUS" = false ] && [ "$LOGS" = false ] && [ "$TEST" = false ]; then
        show_help
    fi
}

# 运行主函数
main "$@" 