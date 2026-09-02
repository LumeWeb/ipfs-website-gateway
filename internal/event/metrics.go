package event

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
)

var (
	sseEventsReceivedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "sse_events_received_total",
		Help: "Total number of SSE events received from portal",
	})
	sseParseErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "sse_parse_errors_total",
		Help: "Total number of SSE events that failed to parse",
	})
	sseErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "sse_client_errors_total",
		Help: "Total number of SSE client errors (connection failures, etc.)",
	})
	sseConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "sse_connected",
		Help: "1 if SSE client is connected to portal, 0 otherwise",
	})
	sseReplayEventsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "sse_replay_events_total",
		Help: "Total number of website changes replayed by the SSE reconciler",
	})
)

func init() {
	metrics.Registerer().MustRegister(
		sseEventsReceivedTotal,
		sseParseErrorsTotal,
		sseErrorsTotal,
		sseConnected,
		sseReplayEventsTotal,
	)
}

// RecordReplayEvents records replayed website changes from the SSE reconciler.
func RecordReplayEvents(n int) {
	sseReplayEventsTotal.Add(float64(n))
}
