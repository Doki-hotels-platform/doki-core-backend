package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTPRequestDuration tracks latency across routes, methods, and status codes.
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "doki",
			Name:      "http_request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"route", "method", "status"},
	)

	// InventoryHoldAcquired tracks inventory hold attempts with result ("success" / "unavailable").
	InventoryHoldAcquired = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "doki",
			Name:      "inventory_hold_acquired_total",
			Help:      "Total inventory hold attempts partitioned by outcome",
		},
		[]string{"result"},
	)

	// InventoryHoldConflict tracks Postgres-layer rejections where Redis hold succeeded but authoritative capacity check failed.
	InventoryHoldConflict = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "doki",
			Name:      "inventory_hold_conflict_total",
			Help:      "Count of Postgres authoritative rejections after Redis fast-path hold succeeded",
		},
	)

	// ReservationHoldExpired tracks the count of expired holds processed by the hold sweeper.
	ReservationHoldExpired = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "doki",
			Name:      "reservation_hold_expired_total",
			Help:      "Total count of reservation holds expired and reclaimed",
		},
	)

	// DBPoolAcquireDuration measures time spent waiting to acquire a connection from pgxpool.
	DBPoolAcquireDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "doki",
			Name:      "db_pool_acquire_duration_seconds",
			Help:      "Time taken to acquire a connection from the pgx connection pool",
			Buckets:   []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5, 1.0},
		},
	)
)
