package smartlimit

import "github.com/prometheus/client_golang/prometheus"

var (
	throttleOpsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubediskguard_cgroup_throttled_ops_total",
			Help: "Total throttled IO operations per container",
		},
		[]string{"container"},
	)
	throttleBytesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubediskguard_cgroup_throttled_bytes_total",
			Help: "Total throttled IO bytes per container",
		},
		[]string{"container"},
	)
	ioPressureStallSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubediskguard_cgroup_io_pressure_stall_seconds",
			Help: "IO pressure stall (avg60) in seconds per container",
		},
		[]string{"container"},
	)
)

func init() {
	prometheus.MustRegister(throttleOpsTotal, throttleBytesTotal, ioPressureStallSeconds)
}

func updateThrottleMetrics(containerID string, opsDelta float64, bytesDelta float64, stallAvg60 float64) {
	if opsDelta > 0 {
		throttleOpsTotal.WithLabelValues(containerID).Add(opsDelta)
	}
	if bytesDelta > 0 {
		throttleBytesTotal.WithLabelValues(containerID).Add(bytesDelta)
	}
	ioPressureStallSeconds.WithLabelValues(containerID).Set(stallAvg60)
}
