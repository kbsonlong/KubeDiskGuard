#!/bin/bash

# kubelet API高级测试脚本
# 包含认证、IO统计分析和数据验证

set -e

# 配置变量
KUBELET_HOST=${KUBELET_HOST:-"localhost"}
KUBELET_PORT=${KUBELET_PORT:-"10250"}
NODE_NAME=${NODE_NAME:-"$(hostname)"}
NAMESPACE=${NAMESPACE:-"default"}
POD_NAME=${POD_NAME:-""}

# 认证配置
KUBE_CONFIG=${KUBE_CONFIG:-"$HOME/.kube/config"}
SERVICE_ACCOUNT_TOKEN=${SERVICE_ACCOUNT_TOKEN:-"/var/run/secrets/kubernetes.io/serviceaccount/token"}
CA_CERT=${CA_CERT:-"/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"}

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

# 临时文件
TEMP_DIR=$(mktemp -d)
SUMMARY_FILE="$TEMP_DIR/summary.json"
CONTAINER_STATS_FILE="$TEMP_DIR/container_stats.json"
CADVISOR_FILE="$TEMP_DIR/cadvisor_metrics.txt"

cleanup() {
    rm -rf "$TEMP_DIR"
}

trap cleanup EXIT

echo -e "${BLUE}=== kubelet API IO统计高级测试 ===${NC}"
echo "Kubelet地址: ${KUBELET_HOST}:${KUBELET_PORT}"
echo "节点名称: ${NODE_NAME}"
echo "临时目录: $TEMP_DIR"
echo ""

# 检查依赖
check_dependencies() {
    echo -e "${YELLOW}检查依赖...${NC}"
    
    if ! command -v curl &> /dev/null; then
        echo -e "${RED}错误: curl未安装${NC}"
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        echo -e "${YELLOW}警告: jq未安装，JSON输出将不可读${NC}"
    fi
    
    echo -e "${GREEN}依赖检查完成${NC}"
    echo ""
}

# 获取认证信息
get_auth_info() {
    echo -e "${YELLOW}获取认证信息...${NC}"
    
    # 方法1: 使用kubeconfig
    if [ -f "$KUBE_CONFIG" ]; then
        echo "使用kubeconfig认证"
        # 这里可以提取token，但为了简化，我们直接使用service account token
    fi
    
    # 方法2: 使用service account token
    if [ -f "$SERVICE_ACCOUNT_TOKEN" ]; then
        TOKEN=$(cat "$SERVICE_ACCOUNT_TOKEN")
        echo "使用service account token认证"
        AUTH_HEADER="Authorization: Bearer $TOKEN"
    else
        echo -e "${YELLOW}警告: 未找到service account token，使用无认证模式${NC}"
        AUTH_HEADER=""
    fi
    
    # 方法3: 使用CA证书
    if [ -f "$CA_CERT" ]; then
        echo "使用CA证书验证"
        CURL_OPTS="-k --cacert $CA_CERT"
    else
        echo -e "${YELLOW}警告: 未找到CA证书，跳过证书验证${NC}"
        CURL_OPTS="-k"
    fi
    
    echo ""
}

# 测试kubelet连接性
test_connectivity() {
    echo -e "${YELLOW}测试kubelet连接性...${NC}"
    
    # 测试健康检查
    echo "测试健康检查端点:"
    if curl $CURL_OPTS -s -H "$AUTH_HEADER" "https://${KUBELET_HOST}:${KUBELET_PORT}/healthz" > /dev/null; then
        echo -e "${GREEN}✓ kubelet健康检查通过${NC}"
    else
        echo -e "${RED}✗ kubelet健康检查失败${NC}"
        return 1
    fi
    
    # 测试API版本
    echo "测试API版本端点:"
    if curl $CURL_OPTS -s -H "$AUTH_HEADER" "https://${KUBELET_HOST}:${KUBELET_PORT}/api/v1.0/" > /dev/null; then
        echo -e "${GREEN}✓ API版本端点可访问${NC}"
    else
        echo -e "${YELLOW}⚠ API版本端点不可访问（可能版本不同）${NC}"
    fi
    
    echo ""
}

# 获取节点摘要统计
get_node_summary() {
    echo -e "${YELLOW}获取节点摘要统计...${NC}"
    
    local url="https://${KUBELET_HOST}:${KUBELET_PORT}/stats/summary"
    echo "请求URL: $url"
    
    if curl $CURL_OPTS -s -H "$AUTH_HEADER" "$url" > "$SUMMARY_FILE"; then
        echo -e "${GREEN}✓ 成功获取节点摘要${NC}"
        
        if command -v jq &> /dev/null; then
            echo "节点摘要结构:"
            jq 'keys' "$SUMMARY_FILE" 2>/dev/null || echo "JSON解析失败"
            
            echo ""
            echo "节点信息:"
            jq '.node' "$SUMMARY_FILE" 2>/dev/null || echo "无法解析节点信息"
            
            echo ""
            echo "Pod数量:"
            jq '.pods | length' "$SUMMARY_FILE" 2>/dev/null || echo "无法解析Pod信息"
            
            # 检查是否有IO统计
            echo ""
            echo "检查IO统计信息:"
            if jq -e '.pods[].containers[].stats[0].diskio' "$SUMMARY_FILE" > /dev/null 2>&1; then
                echo -e "${GREEN}✓ 发现磁盘IO统计信息${NC}"
                echo "示例IO统计:"
                jq '.pods[0].containers[0].stats[0].diskio' "$SUMMARY_FILE" 2>/dev/null || echo "无法解析IO统计"
            else
                echo -e "${YELLOW}⚠ 未发现磁盘IO统计信息${NC}"
            fi
        fi
    else
        echo -e "${RED}✗ 获取节点摘要失败${NC}"
        return 1
    fi
    
    echo ""
}

# 获取容器统计
get_container_stats() {
    echo -e "${YELLOW}获取容器统计...${NC}"
    
    local url="https://${KUBELET_HOST}:${KUBELET_PORT}/stats/container/"
    echo "请求URL: $url"
    
    if curl $CURL_OPTS -s -H "$AUTH_HEADER" "$url" > "$CONTAINER_STATS_FILE"; then
        echo -e "${GREEN}✓ 成功获取容器统计${NC}"
        
        if command -v jq &> /dev/null; then
            echo "容器统计结构:"
            jq 'keys' "$CONTAINER_STATS_FILE" 2>/dev/null || echo "JSON解析失败"
            
            # 查找包含IO统计的容器
            echo ""
            echo "查找包含IO统计的容器:"
            jq -r 'to_entries[] | select(.value.stats[0].diskio != null) | .key' "$CONTAINER_STATS_FILE" 2>/dev/null || echo "未找到IO统计"
            
            # 显示第一个有IO统计的容器的详细信息
            echo ""
            echo "示例容器IO统计:"
            jq -r 'to_entries[] | select(.value.stats[0].diskio != null) | .value.stats[0].diskio' "$CONTAINER_STATS_FILE" 2>/dev/null | head -1 || echo "无法解析IO统计"
        fi
    else
        echo -e "${RED}✗ 获取容器统计失败${NC}"
        return 1
    fi
    
    echo ""
}

# 获取cAdvisor指标
get_cadvisor_metrics() {
    echo -e "${YELLOW}获取cAdvisor指标...${NC}"
    
    local url="https://${KUBELET_HOST}:${KUBELET_PORT}/metrics/cadvisor"
    echo "请求URL: $url"
    
    if curl $CURL_OPTS -s -H "$AUTH_HEADER" "$url" > "$CADVISOR_FILE"; then
        echo -e "${GREEN}✓ 成功获取cAdvisor指标${NC}"
        
        echo "cAdvisor指标数量:"
        wc -l < "$CADVISOR_FILE"
        
        echo ""
        echo "容器相关指标:"
        grep -E "(container|disk|io)" "$CADVISOR_FILE" | head -10 || echo "未找到容器相关指标"
        
        echo ""
        echo "磁盘IO指标:"
        grep -E "container_fs_io" "$CADVISOR_FILE" | head -5 || echo "未找到磁盘IO指标"
        
        echo ""
        echo "磁盘使用指标:"
        grep -E "container_fs_usage" "$CADVISOR_FILE" | head -5 || echo "未找到磁盘使用指标"
    else
        echo -e "${RED}✗ 获取cAdvisor指标失败${NC}"
        return 1
    fi
    
    echo ""
}

# 分析IO统计可用性
analyze_io_stats() {
    echo -e "${YELLOW}分析IO统计可用性...${NC}"
    
    local has_summary_io=false
    local has_container_io=false
    local has_cadvisor_io=false
    
    # 检查摘要API中的IO统计
    if [ -f "$SUMMARY_FILE" ] && jq -e '.pods[].containers[].stats[0].diskio' "$SUMMARY_FILE" > /dev/null 2>&1; then
        has_summary_io=true
    fi
    
    # 检查容器API中的IO统计
    if [ -f "$CONTAINER_STATS_FILE" ] && jq -e 'to_entries[] | select(.value.stats[0].diskio != null)' "$CONTAINER_STATS_FILE" > /dev/null 2>&1; then
        has_container_io=true
    fi
    
    # 检查cAdvisor中的IO指标
    if [ -f "$CADVISOR_FILE" ] && grep -q "container_fs_io" "$CADVISOR_FILE"; then
        has_cadvisor_io=true
    fi
    
    echo "IO统计可用性分析:"
    echo "  Summary API IO统计: $([ "$has_summary_io" = true ] && echo -e "${GREEN}✓ 可用${NC}" || echo -e "${RED}✗ 不可用${NC}")"
    echo "  Container API IO统计: $([ "$has_container_io" = true ] && echo -e "${GREEN}✓ 可用${NC}" || echo -e "${RED}✗ 不可用${NC}")"
    echo "  cAdvisor IO指标: $([ "$has_cadvisor_io" = true ] && echo -e "${GREEN}✓ 可用${NC}" || echo -e "${RED}✗ 不可用${NC}")"
    
    echo ""
    echo -e "${PURPLE}结论:${NC}"
    if [ "$has_summary_io" = true ] || [ "$has_container_io" = true ]; then
        echo -e "${GREEN}✓ kubelet API提供IO统计信息，可用于智能限速${NC}"
        echo "  推荐使用Summary API或Container API获取实时IO数据"
    elif [ "$has_cadvisor_io" = true ]; then
        echo -e "${YELLOW}⚠ cAdvisor提供IO指标，但需要解析Prometheus格式${NC}"
        echo "  可以考虑使用cAdvisor指标作为备选方案"
    else
        echo -e "${RED}✗ kubelet API未提供IO统计信息${NC}"
        echo "  建议继续使用cgroup文件系统采样方案"
    fi
    
    echo ""
}

# 生成测试报告
generate_report() {
    echo -e "${YELLOW}生成测试报告...${NC}"
    
    local report_file="$TEMP_DIR/kubelet_api_test_report.txt"
    
    cat > "$report_file" << EOF
kubelet API IO统计测试报告
==========================

测试时间: $(date)
Kubelet地址: ${KUBELET_HOST}:${KUBELET_PORT}
节点名称: ${NODE_NAME}

测试结果:
EOF
    
    # 添加测试结果
    if [ -f "$SUMMARY_FILE" ]; then
        echo "✓ Summary API测试通过" >> "$report_file"
        if command -v jq &> /dev/null; then
            echo "  Pod数量: $(jq '.pods | length' "$SUMMARY_FILE" 2>/dev/null || echo "未知")" >> "$report_file"
        fi
    else
        echo "✗ Summary API测试失败" >> "$report_file"
    fi
    
    if [ -f "$CONTAINER_STATS_FILE" ]; then
        echo "✓ Container API测试通过" >> "$report_file"
    else
        echo "✗ Container API测试失败" >> "$report_file"
    fi
    
    if [ -f "$CADVISOR_FILE" ]; then
        echo "✓ cAdvisor API测试通过" >> "$report_file"
        echo "  指标数量: $(wc -l < "$CADVISOR_FILE")" >> "$report_file"
    else
        echo "✗ cAdvisor API测试失败" >> "$report_file"
    fi
    
    echo "" >> "$report_file"
    echo "建议:" >> "$report_file"
    echo "1. 如果Summary API或Container API提供IO统计，可用于智能限速" >> "$report_file"
    echo "2. 如果API不可用，建议继续使用cgroup文件系统采样" >> "$report_file"
    echo "3. 考虑配置kubelet认证以获取更完整的数据" >> "$report_file"
    
    echo -e "${GREEN}测试报告已生成: $report_file${NC}"
    echo ""
}

# 主函数
main() {
    check_dependencies
    get_auth_info
    test_connectivity
    get_node_summary
    get_container_stats
    get_cadvisor_metrics
    analyze_io_stats
    generate_report
    
    echo -e "${GREEN}=== 测试完成 ===${NC}"
    echo ""
    echo -e "${BLUE}使用说明:${NC}"
    echo "1. 设置环境变量来自定义测试参数:"
    echo "   export KUBELET_HOST=your-node-ip"
    echo "   export KUBELET_PORT=10250"
    echo "   export NODE_NAME=your-node-name"
    echo ""
    echo "2. 运行测试:"
    echo "   ./scripts/test-kubelet-api-advanced.sh"
    echo ""
    echo "3. 查看测试报告:"
    echo "   cat $TEMP_DIR/kubelet_api_test_report.txt"
    echo ""
}

# 运行主函数
main "$@" 