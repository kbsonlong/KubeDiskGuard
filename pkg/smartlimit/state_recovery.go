package smartlimit

import "log"

// restoreLimitStatus 恢复限速状态
func (m *SmartLimitManager) restoreLimitStatus() {
	log.Println("Restoring limit status from pod annotations...")

	// 获取当前节点上的所有Pod
	pods, err := m.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		log.Printf("Failed to get node pods for status restoration: %v", err)
		return
	}

	restoredCount := 0
	for _, pod := range pods {
		if !m.shouldMonitorPod(pod) {
			continue
		}

		// 检查Pod是否有限速注解
		if m.hasLimitAnnotations(pod.Annotations) {
			// 为每个容器恢复限速状态
			for _, container := range pod.Status.ContainerStatuses {
				if container.ContainerID == "" {
					continue
				}

				containerID := parseContainerID(container.ContainerID)
				if m.restoreContainerLimitStatus(containerID, pod.Name, pod.Namespace, pod.Annotations) {
					restoredCount++
				}
			}
		}
	}

	log.Printf("Restored limit status for %d containers", restoredCount)
}

// restoreContainerLimitStatus 恢复单个容器的限速状态
func (m *SmartLimitManager) restoreContainerLimitStatus(containerID, podName, namespace string, annotations map[string]string) bool {
	prefix := m.config.SmartLimitAnnotationPrefix + "/"

	// 检查是否已被解除限速
	if removed, exists := annotations[prefix+"limit-removed"]; exists && removed == "true" {
		return false
	}

	// 解析触发窗口
	triggeredBy, exists := annotations[prefix+"triggered-by"]
	if !exists {
		return false
	}

	// 解析限速值
	readIOPS := m.parseIntAnnotation(annotations[prefix+"read-iops-limit"], 0)
	writeIOPS := m.parseIntAnnotation(annotations[prefix+"write-iops-limit"], 0)
	readBPS := m.parseIntAnnotation(annotations[prefix+"read-bps-limit"], 0)
	writeBPS := m.parseIntAnnotation(annotations[prefix+"write-bps-limit"], 0)

	// 解析触发原因
	reason := annotations[prefix+"trigger-reason"]

	// 创建限速结果
	limitResult := &LimitResult{
		TriggeredBy: triggeredBy,
		ReadIOPS:    readIOPS,
		WriteIOPS:   writeIOPS,
		ReadBPS:     readBPS,
		WriteBPS:    writeBPS,
		Reason:      reason,
	}

	// 更新限速状态
	m.updateLimitStatus(containerID, podName, namespace, true, limitResult)

	log.Printf("Restored limit status for container %s: %s", containerID, reason)
	return true
}
