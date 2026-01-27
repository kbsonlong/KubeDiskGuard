package throttle

import (
	"time"

	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/kubeclient"
)

type Config interface {
	GetCgroupVersion() string
}

type Detector struct {
	kc      kubeclient.IKubeClient
	rt      container.Runtime
	version string
	stopCh  chan struct{}
}

func NewDetector(kc kubeclient.IKubeClient, rt container.Runtime, cgroupVersion string) *Detector {
	return &Detector{
		kc:      kc,
		rt:      rt,
		version: cgroupVersion,
		stopCh:  make(chan struct{}),
	}
}

func (d *Detector) Start() {
	go d.loop()
}

func (d *Detector) Stop() {
	close(d.stopCh)
}

func (d *Detector) loop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.scanOnce()
		case <-d.stopCh:
			return
		}
	}
}

func (d *Detector) scanOnce() {
	pods, err := d.kc.ListNodePodsWithKubeletFirst()
	if err != nil {
		return
	}
	for _, pod := range pods {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.ContainerID == "" || cs.Started == nil || !*cs.Started {
				continue
			}
			id := extractID(cs.ContainerID)
			if id == "" {
				continue
			}
			if d.version == "v1" {
				ci, err := d.rt.GetContainerByID(id)
				if err != nil {
					continue
				}
				ok, _, _, _, _, _, _, err := d.rt.DetectV1Throttle(ci)
				if err == nil && ok {
					_ = d.kc.CreateEvent(pod.Namespace, pod.Name, "Warning", "CgroupIOLimited", "触发cgroup v1 IO限流")
					d.annotateThrottled(pod.Namespace, pod.Name)
				}
				continue
			}
			opsDelta, bytesDelta, err := d.kc.GetCadvisorThrottleDelta(id, 15*time.Second)
			if err != nil {
				continue
			}
			avg10, avg60, err := d.rt.ReadIOPressure(&container.ContainerInfo{ID: id})
			if err != nil {
				avg10, avg60 = 0, 0
			}
			throttled := (opsDelta > 0 || bytesDelta > 0) || (avg10 > 0 || avg60 > 0)
			if throttled {
				_ = d.kc.CreateEvent(pod.Namespace, pod.Name, "Warning", "CgroupIOLimited", "触发cgroup IO限流")
				d.annotateThrottled(pod.Namespace, pod.Name)
			}
		}
	}
}

func (d *Detector) annotateThrottled(ns, name string) {
	pod, err := d.kc.GetPod(ns, name)
	if err != nil {
		return
	}
	ann := make(map[string]string)
	for k, v := range pod.Annotations {
		ann[k] = v
	}
	ann["io-limit/throttled"] = "true"
	pod.Annotations = ann
	_, _ = d.kc.UpdatePod(pod)
}

func extractID(k8sID string) string {
	if k8sID == "" {
		return ""
	}
	if idx := len("docker://"); len(k8sID) > idx && k8sID[:idx] == "docker://" {
		return k8sID[idx:]
	}
	if idx := len("containerd://"); len(k8sID) > idx && k8sID[:idx] == "containerd://" {
		return k8sID[idx:]
	}
	return k8sID
}
