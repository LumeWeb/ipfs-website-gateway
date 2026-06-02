package cache

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
)

const (
	MetricStatusHitsTotal    = "status_hits_total"
	MetricStatusMissesTotal  = "status_misses_total"
	MetricStatusExpiredTotal = "status_expired_total"
)

var (
	statusCacheHitsTotal    prometheus.Counter
	statusCacheMissesTotal  prometheus.Counter
	statusCacheExpiredTotal prometheus.Counter
)

func init() {
	statusCacheHitsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "cache",
		Name:      MetricStatusHitsTotal,
		Help:      "Total number of status cache hits",
	})
	metrics.MustRegister(statusCacheHitsTotal)

	statusCacheMissesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "cache",
		Name:      MetricStatusMissesTotal,
		Help:      "Total number of status cache misses",
	})
	metrics.MustRegister(statusCacheMissesTotal)

	statusCacheExpiredTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: "cache",
		Name:      MetricStatusExpiredTotal,
		Help:      "Total number of status cache expired entries served",
	})
	metrics.MustRegister(statusCacheExpiredTotal)
}
