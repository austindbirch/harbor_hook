package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Events Published with tenant_id label (Phase 5 requirement)
	EventsPublishedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harborhook_events_published_total",
			Help: "Total number of events published by tenant.",
		},
		[]string{"tenant_id"},
	)

	// Deliveries with status, tenant_id, and endpoint_id labels (Phase 5 requirement)
	DeliveriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harborhook_deliveries_total",
			Help: "Total number of deliveries by status, tenant, and endpoint.",
		},
		[]string{"status", "tenant_id", "endpoint_id"},
	)

	// Delivery latency histogram with tenant_id label (Phase 5 requirement)
	DeliveryLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harborhook_delivery_latency_seconds",
			Help:    "Delivery latency in seconds by tenant.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~32s
		},
		[]string{"tenant_id"},
	)

	// Note: WorkerBacklog moved to dedicated nsq-monitor service

	// Retries with reason label (Phase 5 requirement)
	RetriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harborhook_retries_total",
			Help: "Total number of delivery retries by reason.",
		},
		[]string{"reason"}, // e.g. http_5xx, timeout, network, dns_error, connection_refused, other
	)

	// DLQ entries with reason label (Phase 5 requirement)
	DLQTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harborhook_dlq_total",
			Help: "Total number of deliveries moved to DLQ by reason.",
		},
		[]string{"reason"},
	)

	// HTTP response time for webhook deliveries
	HTTPDeliveryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harborhook_http_delivery_duration_seconds",
			Help:    "HTTP delivery request duration in seconds.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~32s
		},
		[]string{"tenant_id", "endpoint_id", "status_code"},
	)

	// NSQ topic depth (optional Phase 5 requirement)
	NSQTopicDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "harborhook_nsq_topic_depth",
			Help: "Current depth of NSQ topics.",
		},
		[]string{"topic", "channel"},
	)
)

// MustRegister registers all metrics with the provided registry
func MustRegister(reg *prometheus.Registry) {
	reg.MustRegister(
		EventsPublishedTotal,
		DeliveriesTotal,
		DeliveryLatencySeconds,
		RetriesTotal,
		DLQTotal,
		HTTPDeliveryDuration,
		NSQTopicDepth,
	)
}

// Helper functions for recording common metric patterns

// RecordEventPublished increments the events published counter
func RecordEventPublished(tenantID string) {
	EventsPublishedTotal.WithLabelValues(tenantID).Inc()
}

// RecordDelivery increments delivery counter and records latency
func RecordDelivery(status, tenantID, endpointID string, duration time.Duration) {
	DeliveriesTotal.WithLabelValues(status, tenantID, endpointID).Inc()
	DeliveryLatencySeconds.WithLabelValues(tenantID).Observe(duration.Seconds())
}

// RecordHTTPDelivery records HTTP delivery metrics
func RecordHTTPDelivery(tenantID, endpointID, statusCode string, duration time.Duration) {
	HTTPDeliveryDuration.WithLabelValues(tenantID, endpointID, statusCode).Observe(duration.Seconds())
}

// RecordRetry increments retry counter with reason
func RecordRetry(reason string) {
	RetriesTotal.WithLabelValues(reason).Inc()
}

// RecordDLQ increments DLQ counter with reason
func RecordDLQ(reason string) {
	DLQTotal.WithLabelValues(reason).Inc()
}

// Note: UpdateWorkerBacklog removed - now handled by nsq-monitor service

// UpdateNSQTopicDepth updates NSQ topic depth
func UpdateNSQTopicDepth(topic, channel string, depth float64) {
	NSQTopicDepth.WithLabelValues(topic, channel).Set(depth)
}
