package prewarm

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
)

const (
	MetricPrewarmSubmittedTotal      = "submitted_total"
	MetricPrewarmCompletedTotal      = "completed_total"
	MetricPrewarmFailedTotal         = "failed_total"
	MetricPrewarmBlocksFetched       = "blocks_fetched_total"
	MetricPrewarmActiveWalks         = "active_walks"
	MetricPrewarmPathSubmittedTotal  = "path_submitted_total"
	MetricPrewarmDAGBatchUsed        = "dag_batch_used_total"
	MetricPrewarmDAGBatchFallback     = "dag_batch_fallback_total"
)

var (
	prewarmSubmittedTotal     prometheus.Counter
	prewarmCompletedTotal     prometheus.Counter
	prewarmFailedTotal        prometheus.Counter
	prewarmBlocksFetched      prometheus.Counter
	prewarmActiveWalks        prometheus.Gauge
	prewarmPathSubmittedTotal prometheus.Counter
	prewarmDAGBatchUsed       prometheus.Counter
	prewarmDAGBatchFallback   prometheus.Counter
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

	prewarmPathSubmittedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "prewarm",
		Name:      MetricPrewarmPathSubmittedTotal,
		Help:      "Total number of path-targeted prewarm submissions",
	})
	metrics.MustRegister(prewarmPathSubmittedTotal)

	prewarmDAGBatchUsed = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "prewarm",
		Name:      MetricPrewarmDAGBatchUsed,
		Help:      "Total number of prewarms that used the DAG-bypass batch path",
	})
	metrics.MustRegister(prewarmDAGBatchUsed)

	prewarmDAGBatchFallback = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "prewarm",
		Name:      MetricPrewarmDAGBatchFallback,
		Help:      "Total number of prewarms that fell back to sequential walk after DAG-bypass failed",
	})
	metrics.MustRegister(prewarmDAGBatchFallback)
}
