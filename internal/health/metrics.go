package health

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
)

var (
	// healthCheckStatus: 1 = up, 0 = down. Labels: check name, domain (empty for non-domain checks).
	healthCheckStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ipfs_gateway_health_check_status",
			Help: "Health check status: 1 = up, 0 = down.",
		},
		[]string{"check", "domain"},
	)

	// healthCheckDuration: seconds the check took to run.
	healthCheckDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ipfs_gateway_health_check_duration_seconds",
			Help: "Duration of the last health check execution in seconds.",
		},
		[]string{"check", "domain"},
	)
)

func init() {
	metrics.MustRegister(healthCheckStatus, healthCheckDuration)
}

// setCheckResult updates the Prometheus gauges for a check result.
func setCheckResult(check, domain string, up bool, durationSeconds float64) {
	val := 0.0
	if up {
		val = 1.0
	}
	healthCheckStatus.WithLabelValues(check, domain).Set(val)
	healthCheckDuration.WithLabelValues(check, domain).Set(durationSeconds)
}
