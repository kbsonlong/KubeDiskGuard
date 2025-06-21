#!/bin/bash

# kubelet API测试脚本
# 用于测试kubelet API是否提供IO统计信息

set -e

# 配置变量
KUBELET_HOST=${KUBELET_HOST:-"localhost"}
KUBELET_PORT=${KUBELET_PORT:-"10250"}
NODE_NAME=${NODE_NAME:-"$(hostname)"}
NAMESPACE=${NAMESPACE:-"default"}
POD_NAME=${POD_NAME:-""}

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== kubelet API IO统计测试 ===${NC}"
echo "Kubelet地址: ${KUBELET_HOST}:${KUBELET_PORT}"
echo "节点名称: ${NODE_NAME}"
echo ""

# 1. 测试节点摘要API (Summary API)
echo -e "${YELLOW}1. 测试节点摘要API...${NC}"
echo "URL: https://${KUBELET_HOST}:${KUBELET_PORT}/stats/summary"
echo ""

# 尝试获取节点摘要
echo "获取节点摘要信息:"
curl -k -s "https://${KUBELET_HOST}:${KUBELET_PORT}/stats/summary" | jq '.' 2>/dev/null || {
    echo -e "${RED}获取节点摘要失败，尝试其他端点...${NC}"
    echo ""
}

# 2. 测试容器统计API
echo -e "${YELLOW}2. 测试容器统计API...${NC}"
echo "URL: https://${KUBELET_HOST}:${KUBELET_PORT}/stats/container/${NAMESPACE}/${POD_NAME}"
echo ""

if [ -n "$POD_NAME" ]; then
    echo "获取指定Pod容器统计:"
    curl -k -s "https://${KUBELET_HOST}:${KUBELET_PORT}/stats/container/${NAMESPACE}/${POD_NAME}" | jq '.' 2>/dev/null || {
        echo -e "${RED}获取指定Pod统计失败${NC}"
    }
    echo ""
fi

# 3. 测试所有容器统计
echo -e "${YELLOW}3. 测试所有容器统计...${NC}"
echo "URL: https://${KUBELET_HOST}:${KUBELET_PORT}/stats/container/"
echo ""

echo "获取所有容器统计信息:"
curl -k -s "https://${KUBELET_HOST}:${KUBELET_PORT}/stats/container/" | jq '.' 2>/dev/null || {
    echo -e "${RED}获取所有容器统计失败${NC}"
}
echo ""

# 4. 测试cAdvisor API
echo -e "${YELLOW}4. 测试cAdvisor API...${NC}"
echo "URL: https://${KUBELET_HOST}:${KUBELET_PORT}/metrics/cadvisor"
echo ""

echo "获取cAdvisor指标:"
curl -k -s "https://${KUBELET_HOST}:${KUBELET_PORT}/metrics/cadvisor" | head -50 2>/dev/null || {
    echo -e "${RED}获取cAdvisor指标失败${NC}"
}
echo ""

# 5. 测试kubelet指标API
echo -e "${YELLOW}5. 测试kubelet指标API...${NC}"
echo "URL: https://${KUBELET_HOST}:${KUBELET_PORT}/metrics"
echo ""

echo "获取kubelet指标:"
curl -k -s "https://${KUBELET_HOST}:${KUBELET_PORT}/metrics" | grep -E "(container|disk|io)" | head -20 2>/dev/null || {
    echo -e "${RED}获取kubelet指标失败${NC}"
}
echo ""

# 6. 测试健康检查
echo -e "${YELLOW}6. 测试kubelet健康状态...${NC}"
echo "URL: https://${KUBELET_HOST}:${KUBELET_PORT}/healthz"
echo ""

echo "kubelet健康状态:"
curl -k -s "https://${KUBELET_HOST}:${KUBELET_PORT}/healthz" 2>/dev/null || {
    echo -e "${RED}kubelet健康检查失败${NC}"
}
echo ""

# 7. 测试API版本
echo -e "${YELLOW}7. 测试API版本...${NC}"
echo "URL: https://${KUBELET_HOST}:${KUBELET_PORT}/api/v1.0/"
echo ""

echo "API版本信息:"
curl -k -s "https://${KUBELET_HOST}:${KUBELET_PORT}/api/v1.0/" 2>/dev/null || {
    echo -e "${RED}获取API版本失败${NC}"
}
echo ""

echo -e "${GREEN}=== 测试完成 ===${NC}"
echo ""
echo -e "${BLUE}使用说明:${NC}"
echo "1. 设置环境变量来自定义测试参数:"
echo "   export KUBELET_HOST=your-node-ip"
echo "   export KUBELET_PORT=10250"
echo "   export NODE_NAME=your-node-name"
echo "   export NAMESPACE=your-namespace"
echo "   export POD_NAME=your-pod-name"
echo ""
echo "2. 运行测试:"
echo "   ./scripts/test-kubelet-api.sh"
echo ""
echo "3. 如果kubelet需要认证，可能需要配置证书或token"
echo "" 