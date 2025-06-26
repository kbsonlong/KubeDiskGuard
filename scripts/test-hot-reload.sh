#!/bin/bash

# 热重载功能测试脚本
# 用于测试配置文件的动态更新是否能够触发服务热重载

set -e

echo "=== KubeDiskGuard 热重载功能测试 ==="

# 检查必要的环境变量
if [ -z "$NODE_NAME" ]; then
    echo "错误: 请设置 NODE_NAME 环境变量"
    exit 1
fi

# 创建临时配置文件目录
TEMP_CONFIG_DIR=$(mktemp -d)
echo "临时配置文件目录: $TEMP_CONFIG_DIR"

# 清理函数
cleanup() {
    echo "清理临时文件..."
    rm -rf "$TEMP_CONFIG_DIR"
    if [ ! -z "$PID" ]; then
        echo "停止测试进程 PID: $PID"
        kill $PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# 创建初始配置文件
cat > "$TEMP_CONFIG_DIR/config.json" << EOF
{
    "container_iops_limit": 1000,
    "container_read_iops_limit": 500,
    "container_write_iops_limit": 500,
    "container_bps_limit": 104857600,
    "container_read_bps_limit": 52428800,
    "container_write_bps_limit": 52428800,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 30,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "smart_limit_windows": [
        {
            "duration": 15,
            "iops_threshold": 800,
            "bps_threshold": 83886080
        },
        {
            "duration": 30,
            "iops_threshold": 600,
            "bps_threshold": 62914560
        }
    ],
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"],
    "data_mount": "/var/lib/docker",
    "container_runtime": "auto",
    "cgroup_version": "auto"
}
EOF

echo "初始配置文件已创建"

# 启动服务（后台运行）
echo "启动 KubeDiskGuard 服务..."
export CONFIG_FILE_PATH="$TEMP_CONFIG_DIR/config.json"
export NODE_NAME="$NODE_NAME"

# 启动服务并捕获PID
./iops-limit-service -metrics-addr :2112 > "$TEMP_CONFIG_DIR/service.log" 2>&1 &
PID=$!

echo "服务已启动，PID: $PID"

# 等待服务启动
echo "等待服务启动..."
sleep 5

# 检查服务是否正常启动
if ! curl -s http://localhost:2112/healthz > /dev/null; then
    echo "错误: 服务启动失败，请检查日志:"
    cat "$TEMP_CONFIG_DIR/service.log"
    exit 1
fi

echo "服务启动成功"

# 测试1: 更新IOPS限制
echo ""
echo "=== 测试1: 更新IOPS限制 ==="
echo "当前IOPS限制: 1000"
echo "更新为: 2000"

cat > "$TEMP_CONFIG_DIR/config.json" << EOF
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "container_bps_limit": 104857600,
    "container_read_bps_limit": 52428800,
    "container_write_bps_limit": 52428800,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 30,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "smart_limit_windows": [
        {
            "duration": 15,
            "iops_threshold": 800,
            "bps_threshold": 83886080
        },
        {
            "duration": 30,
            "iops_threshold": 600,
            "bps_threshold": 62914560
        }
    ],
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"],
    "data_mount": "/var/lib/docker",
    "container_runtime": "auto",
    "cgroup_version": "auto"
}
EOF

echo "配置文件已更新，等待热重载..."
sleep 10

# 检查日志中是否有热重载信息
if grep -q "热重载" "$TEMP_CONFIG_DIR/service.log"; then
    echo "✓ 热重载成功触发"
else
    echo "✗ 热重载未触发"
fi

# 测试2: 更新监控间隔
echo ""
echo "=== 测试2: 更新监控间隔 ==="
echo "当前监控间隔: 30秒"
echo "更新为: 60秒"

cat > "$TEMP_CONFIG_DIR/config.json" << EOF
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "container_bps_limit": 104857600,
    "container_read_bps_limit": 52428800,
    "container_write_bps_limit": 52428800,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 60,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "smart_limit_windows": [
        {
            "duration": 15,
            "iops_threshold": 800,
            "bps_threshold": 83886080
        },
        {
            "duration": 30,
            "iops_threshold": 600,
            "bps_threshold": 62914560
        }
    ],
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"],
    "data_mount": "/var/lib/docker",
    "container_runtime": "auto",
    "cgroup_version": "auto"
}
EOF

echo "配置文件已更新，等待热重载..."
sleep 10

# 检查日志中是否有监控循环重启信息
if grep -q "监控循环已重启" "$TEMP_CONFIG_DIR/service.log"; then
    echo "✓ 监控循环重启成功"
else
    echo "✗ 监控循环重启未触发"
fi

# 测试3: 禁用智能限速
echo ""
echo "=== 测试3: 禁用智能限速 ==="
echo "当前智能限速: 启用"
echo "更新为: 禁用"

cat > "$TEMP_CONFIG_DIR/config.json" << EOF
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "container_bps_limit": 104857600,
    "container_read_bps_limit": 52428800,
    "container_write_bps_limit": 52428800,
    "smart_limit_enabled": false,
    "smart_limit_monitor_interval": 60,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "smart_limit_windows": [
        {
            "duration": 15,
            "iops_threshold": 800,
            "bps_threshold": 83886080
        },
        {
            "duration": 30,
            "iops_threshold": 600,
            "bps_threshold": 62914560
        }
    ],
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"],
    "data_mount": "/var/lib/docker",
    "container_runtime": "auto",
    "cgroup_version": "auto"
}
EOF

echo "配置文件已更新，等待热重载..."
sleep 10

# 检查日志中是否有智能限速停止信息
if grep -q "智能限速管理器已停止" "$TEMP_CONFIG_DIR/service.log"; then
    echo "✓ 智能限速禁用成功"
else
    echo "✗ 智能限速禁用未触发"
fi

# 测试4: 重新启用智能限速
echo ""
echo "=== 测试4: 重新启用智能限速 ==="
echo "当前智能限速: 禁用"
echo "更新为: 启用"

cat > "$TEMP_CONFIG_DIR/config.json" << EOF
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "container_bps_limit": 104857600,
    "container_read_bps_limit": 52428800,
    "container_write_bps_limit": 52428800,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 60,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "smart_limit_windows": [
        {
            "duration": 15,
            "iops_threshold": 800,
            "bps_threshold": 83886080
        },
        {
            "duration": 30,
            "iops_threshold": 600,
            "bps_threshold": 62914560
        }
    ],
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"],
    "data_mount": "/var/lib/docker",
    "container_runtime": "auto",
    "cgroup_version": "auto"
}
EOF

echo "配置文件已更新，等待热重载..."
sleep 10

# 检查日志中是否有智能限速启动信息
if grep -q "智能限速管理器已启动" "$TEMP_CONFIG_DIR/service.log"; then
    echo "✓ 智能限速重新启用成功"
else
    echo "✗ 智能限速重新启用未触发"
fi

echo ""
echo "=== 热重载测试完成 ==="
echo "服务日志摘要:"
echo "=================="
tail -20 "$TEMP_CONFIG_DIR/service.log"

echo ""
echo "所有测试完成！" 