package profiler

import "github.com/prometheus/client_golang/prometheus"

var (
	runsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kubediskguard_profiler_runs_total",
		Help: "Number of profiler sampling runs",
	})
	skipsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kubediskguard_profiler_skips_total",
		Help: "Number of profiler skips due to high load or errors",
	})
	durationSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kubediskguard_profiler_last_duration_seconds",
		Help: "Duration of last profiler sampling run in seconds",
	})
)

func init() {
	prometheus.MustRegister(runsTotal, skipsTotal, durationSeconds)
}

func MarkRunStart() {
	runsTotal.Inc()
}

func MarkSkip() {
	skipsTotal.Inc()
}

func MarkDuration(sec float64) {
	durationSeconds.Set(sec)
}
