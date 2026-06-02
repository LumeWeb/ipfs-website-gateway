package gateway

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
)

const (
	MetricAccessCheckDuration = "access_check_duration_seconds"
	MetricAccessCheckTotal    = "access_check_total"

	LabelResultCacheHit    = "cache_hit"
	LabelResultCacheExpired = "cache_expired"
	LabelResultCacheMiss   = "cache_miss"
	LabelResultAPISuccess  = "api_success"
	LabelResultError       = "error"
)

var (
	accessCheckDuration *prometheus.HistogramVec
	accessCheckTotal    *prometheus.CounterVec
)

func init() {
	accessCheckDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: "gateway",
			Name:      MetricAccessCheckDuration,
			Help:      "Time spent checking website access",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"result"},
	)
	metrics.MustRegister(accessCheckDuration)

	accessCheckTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "gateway",
			Name:      MetricAccessCheckTotal,
			Help:      "Total number of access checks",
		},
		[]string{"result"},
	)
	metrics.MustRegister(accessCheckTotal)
}
