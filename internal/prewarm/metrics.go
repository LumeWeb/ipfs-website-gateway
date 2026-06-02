package prewarm

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
)

const (
	MetricPrewarmSubmittedTotal = "submitted_total"
	MetricPrewarmCompletedTotal = "completed_total"
	MetricPrewarmFailedTotal    = "failed_total"
	MetricPrewarmBlocksFetched  = "blocks_fetched_total"
	MetricPrewarmActiveWalks    = "active_walks"
)

var (
	prewarmSubmittedTotal prometheus.Counter
	prewarmCompletedTotal prometheus.Counter
	prewarmFailedTotal    prometheus.Counter
	prewarmBlocksFetched  prometheus.Counter
	prewarmActiveWalks    prometheus.Gauge
)

func init() {
	prewarmSubmittedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "prewarm",
		Name:      MetricPrewarmSubmittedTotal,
		Help:      "Total number of DAG walks submitted",
	})
	metrics.MustRegister(prewarmSubmittedTotal)

	prewarmCompletedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "prewarm",
		Name:      MetricPrewarmCompletedTotal,
		Help:      "Total number of DAG walks completed",
	})
	metrics.MustRegister(prewarmCompletedTotal)

	prewarmFailedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "prewarm",
		Name:      MetricPrewarmFailedTotal,
		Help:      "Total number of DAG walks that failed",
	})
	metrics.MustRegister(prewarmFailedTotal)

	prewarmBlocksFetched = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "prewarm",
		Name:      MetricPrewarmBlocksFetched,
		Help:      "Total number of blocks fetched during prewarming",
	})
	metrics.MustRegister(prewarmBlocksFetched)

	prewarmActiveWalks = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: "prewarm",
		Name:      MetricPrewarmActiveWalks,
		Help:      "Number of currently active DAG walks",
	})
	metrics.MustRegister(prewarmActiveWalks)
}
