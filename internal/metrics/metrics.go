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
)

func MustRegister(reg *prometheus.Registry) {
	reg.MustRegister(EventsPublishedTotal, DeliveriesTotal)
}
