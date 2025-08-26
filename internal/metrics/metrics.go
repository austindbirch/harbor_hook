package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	EventsPublishedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "harborhook_events_published_total",
			Help: "Total number of events published.",
		},
	)

	DeliveriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harborhook_deliveries_total",
			Help: "Total number of deliveries by status.",
		},
		[]string{"status"},
	)

	RetriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harborhook_retries_total",
			Help: "Total number of delivery retries by reason.",
		},
		[]string{"reason"}, // e.g. http_5xx, timeout, network, other
	)

	DLQTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "harborhook_dlq_total",
			Help: "Total number of deliveries moved to DLQ.",
		},
	)
)

func MustRegister(reg *prometheus.Registry) {
	reg.MustRegister(EventsPublishedTotal, DeliveriesTotal, RetriesTotal, DLQTotal)
}
