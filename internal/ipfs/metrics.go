package ipfs

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
)

var (
	seedPeerConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ipfs_gateway_seed_peer_connected",
		Help: "1 if the seed peer is connected, 0 otherwise.",
	})

	seedPeerConnectAttempts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ipfs_gateway_seed_peer_connect_attempts_total",
		Help: "Total number of seed peer connection attempts.",
	})

	seedPeerConnectErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ipfs_gateway_seed_peer_connect_errors_total",
		Help: "Total number of seed peer connection failures.",
	})

	seedPeerDisconnects = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ipfs_gateway_seed_peer_disconnects_total",
		Help: "Total number of times the seed peer disconnected unexpectedly.",
	})
)

func init() {
	metrics.MustRegister(
		seedPeerConnected,
		seedPeerConnectAttempts,
		seedPeerConnectErrors,
		seedPeerDisconnects,
	)
}
