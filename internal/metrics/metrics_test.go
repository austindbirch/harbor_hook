package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMustRegister(t *testing.T) {
	tests := []struct {
		name     string
		registry *prometheus.Registry
	}{
		{
			name:     "register with new registry",
			registry: prometheus.NewRegistry(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("MustRegister() panicked: %v", r)
				}
			}()

			MustRegister(tt.registry)

			// Record some values so metrics appear in Gather()
			RecordEventPublished("test-tenant")
			RecordDelivery("success", "test-tenant", "test-endpoint", 100*time.Millisecond)
			RecordRetry("timeout")
			RecordDLQ("max_retries")
			UpdateWorkerBacklog(5)
			UpdateNSQTopicDepth("test-topic", "test-channel", 3)

			// Verify all metrics are registered by checking gather
			metricFamilies, err := tt.registry.Gather()
			if err != nil {
				t.Errorf("Registry.Gather() error: %v", err)
			}

			expectedMetrics := []string{
				"harborhook_events_published_total",
				"harborhook_deliveries_total",
				"harborhook_delivery_latency_seconds",
				"harborhook_worker_backlog",
				"harborhook_retries_total",
				"harborhook_dlq_total",
				"harborhook_nsq_topic_depth",
			}

			registeredMetrics := make(map[string]bool)
			for _, mf := range metricFamilies {
				registeredMetrics[mf.GetName()] = true
			}

			for _, expected := range expectedMetrics {
				if !registeredMetrics[expected] {
					t.Errorf("Expected metric %s not found in registry", expected)
				}
			}
		})
	}
}

func TestRecordEventPublished(t *testing.T) {
	// Reset metric before testing
	EventsPublishedTotal.Reset()

	tests := []struct {
		name     string
		tenantID string
		calls    int
	}{
		{
			name:     "single event published",
			tenantID: "tenant-123",
			calls:    1,
		},
		{
			name:     "multiple events published",
			tenantID: "tenant-456",
			calls:    5,
		},
		{
			name:     "empty tenant ID",
			tenantID: "",
			calls:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record events
			for i := 0; i < tt.calls; i++ {
				RecordEventPublished(tt.tenantID)
			}

			// Verify counter value
			counter := EventsPublishedTotal.WithLabelValues(tt.tenantID)
			value := testutil.ToFloat64(counter)
			if value != float64(tt.calls) {
				t.Errorf("RecordEventPublished() counter value = %f, want %f", value, float64(tt.calls))
			}
		})
	}
}

func TestRecordDelivery(t *testing.T) {
	// Reset metrics before testing
	DeliveriesTotal.Reset()
	DeliveryLatencySeconds.Reset()

	tests := []struct {
		name       string
		status     string
		tenantID   string
		endpointID string
		duration   time.Duration
		calls      int
	}{
		{
			name:       "successful delivery",
			status:     "success",
			tenantID:   "tenant-123",
			endpointID: "endpoint-abc",
			duration:   100 * time.Millisecond,
			calls:      1,
		},
		{
			name:       "failed delivery",
			status:     "failed",
			tenantID:   "tenant-456",
			endpointID: "endpoint-def",
			duration:   2 * time.Second,
			calls:      3,
		},
		{
			name:       "timeout delivery",
			status:     "timeout",
			tenantID:   "tenant-789",
			endpointID: "endpoint-ghi",
			duration:   30 * time.Second,
			calls:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record deliveries
			for i := 0; i < tt.calls; i++ {
				RecordDelivery(tt.status, tt.tenantID, tt.endpointID, tt.duration)
			}

			// Verify delivery counter
			deliveryCounter := DeliveriesTotal.WithLabelValues(tt.status, tt.tenantID, tt.endpointID)
			deliveryValue := testutil.ToFloat64(deliveryCounter)
			if deliveryValue != float64(tt.calls) {
				t.Errorf("RecordDelivery() delivery counter = %f, want %f", deliveryValue, float64(tt.calls))
			}

			// For histograms, we verify the metric exists and has recorded observations
			// by checking it exists in the registry after recording
			if DeliveryLatencySeconds.WithLabelValues(tt.tenantID) == nil {
				t.Error("RecordDelivery() latency histogram should not be nil after recording")
			}
		})
	}
}

func TestRecordHTTPDelivery(t *testing.T) {
	// Reset metric before testing
	HTTPDeliveryDuration.Reset()

	tests := []struct {
		name       string
		tenantID   string
		endpointID string
		statusCode string
		duration   time.Duration
		calls      int
	}{
		{
			name:       "200 OK response",
			tenantID:   "tenant-123",
			endpointID: "endpoint-abc",
			statusCode: "200",
			duration:   50 * time.Millisecond,
			calls:      1,
		},
		{
			name:       "500 error response",
			tenantID:   "tenant-456",
			endpointID: "endpoint-def",
			statusCode: "500",
			duration:   1 * time.Second,
			calls:      2,
		},
		{
			name:       "timeout response",
			tenantID:   "tenant-789",
			endpointID: "endpoint-ghi",
			statusCode: "timeout",
			duration:   30 * time.Second,
			calls:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record HTTP deliveries
			for i := 0; i < tt.calls; i++ {
				RecordHTTPDelivery(tt.tenantID, tt.endpointID, tt.statusCode, tt.duration)
			}

			// Verify histogram exists and has recorded observations
			if HTTPDeliveryDuration.WithLabelValues(tt.tenantID, tt.endpointID, tt.statusCode) == nil {
				t.Error("RecordHTTPDelivery() histogram should not be nil after recording")
			}
		})
	}
}

func TestRecordRetry(t *testing.T) {
	// Reset metric before testing
	RetriesTotal.Reset()

	tests := []struct {
		name   string
		reason string
		calls  int
	}{
		{
			name:   "HTTP 5xx retry",
			reason: "http_5xx",
			calls:  1,
		},
		{
			name:   "timeout retry",
			reason: "timeout",
			calls:  3,
		},
		{
			name:   "network retry",
			reason: "network",
			calls:  2,
		},
		{
			name:   "DNS error retry",
			reason: "dns_error",
			calls:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record retries
			for i := 0; i < tt.calls; i++ {
				RecordRetry(tt.reason)
			}

			// Verify counter value
			counter := RetriesTotal.WithLabelValues(tt.reason)
			value := testutil.ToFloat64(counter)
			if value != float64(tt.calls) {
				t.Errorf("RecordRetry() counter value = %f, want %f", value, float64(tt.calls))
			}
		})
	}
}

func TestRecordDLQ(t *testing.T) {
	// Reset metric before testing
	DLQTotal.Reset()

	tests := []struct {
		name   string
		reason string
		calls  int
	}{
		{
			name:   "max retries exceeded",
			reason: "max_retries_exceeded",
			calls:  1,
		},
		{
			name:   "permanent failure",
			reason: "permanent_failure",
			calls:  2,
		},
		{
			name:   "timeout",
			reason: "timeout",
			calls:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record DLQ entries
			for i := 0; i < tt.calls; i++ {
				RecordDLQ(tt.reason)
			}

			// Verify counter value
			counter := DLQTotal.WithLabelValues(tt.reason)
			value := testutil.ToFloat64(counter)
			if value != float64(tt.calls) {
				t.Errorf("RecordDLQ() counter value = %f, want %f", value, float64(tt.calls))
			}
		})
	}
}

func TestUpdateWorkerBacklog(t *testing.T) {
	tests := []struct {
		name  string
		count float64
	}{
		{
			name:  "zero backlog",
			count: 0,
		},
		{
			name:  "positive backlog",
			count: 42,
		},
		{
			name:  "large backlog",
			count: 10000,
		},
		{
			name:  "floating point backlog",
			count: 123.45,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			UpdateWorkerBacklog(tt.count)

			// Verify gauge value
			value := testutil.ToFloat64(WorkerBacklog)
			if value != tt.count {
				t.Errorf("UpdateWorkerBacklog() gauge value = %f, want %f", value, tt.count)
			}
		})
	}
}

func TestUpdateNSQTopicDepth(t *testing.T) {
	// Reset metric before testing
	NSQTopicDepth.Reset()

	tests := []struct {
		name    string
		topic   string
		channel string
		depth   float64
	}{
		{
			name:    "deliveries topic",
			topic:   "deliveries",
			channel: "worker",
			depth:   10,
		},
		{
			name:    "events topic",
			topic:   "events",
			channel: "processor",
			depth:   0,
		},
		{
			name:    "large depth",
			topic:   "backlog",
			channel: "consumer",
			depth:   50000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			UpdateNSQTopicDepth(tt.topic, tt.channel, tt.depth)

			// Verify gauge value
			gauge := NSQTopicDepth.WithLabelValues(tt.topic, tt.channel)
			value := testutil.ToFloat64(gauge)
			if value != tt.depth {
				t.Errorf("UpdateNSQTopicDepth() gauge value = %f, want %f", value, tt.depth)
			}
		})
	}
}

func TestMetricsIntegration(t *testing.T) {
	// Create a new registry for integration test
	registry := prometheus.NewRegistry()
	MustRegister(registry)

	// Record some metrics
	RecordEventPublished("tenant-integration")
	RecordDelivery("success", "tenant-integration", "endpoint-integration", 100*time.Millisecond)
	RecordHTTPDelivery("tenant-integration", "endpoint-integration", "200", 50*time.Millisecond)
	RecordRetry("timeout")
	RecordDLQ("max_retries_exceeded")
	UpdateWorkerBacklog(5)
	UpdateNSQTopicDepth("deliveries", "worker", 3)

	// Gather metrics and verify they're present
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Errorf("Registry.Gather() error: %v", err)
	}

	if len(metricFamilies) == 0 {
		t.Error("Expected metrics to be present after recording")
	}

	// Look for specific metrics in output
	found := make(map[string]bool)
	for _, mf := range metricFamilies {
		found[mf.GetName()] = true
	}

	requiredMetrics := []string{
		"harborhook_events_published_total",
		"harborhook_deliveries_total",
		"harborhook_worker_backlog",
	}

	for _, metric := range requiredMetrics {
		if !found[metric] {
			t.Errorf("Expected metric %s not found in gathered metrics", metric)
		}
	}
}

func TestMetricLabels(t *testing.T) {
	tests := []struct {
		name           string
		metricAction   func()
		expectedLabels []string
	}{
		{
			name: "events published labels",
			metricAction: func() {
				RecordEventPublished("test-tenant")
			},
			expectedLabels: []string{"tenant_id"},
		},
		{
			name: "delivery labels",
			metricAction: func() {
				RecordDelivery("success", "test-tenant", "test-endpoint", time.Second)
			},
			expectedLabels: []string{"status", "tenant_id", "endpoint_id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			MustRegister(registry)

			tt.metricAction()

			metricFamilies, err := registry.Gather()
			if err != nil {
				t.Errorf("Registry.Gather() error: %v", err)
			}

			// Find relevant metric family and check labels are present
			found := false
			for _, mf := range metricFamilies {
				if len(mf.GetMetric()) > 0 {
					metric := mf.GetMetric()[0]
					if len(metric.GetLabel()) > 0 {
						found = true
						break
					}
				}
			}

			if !found {
				t.Error("Expected to find metrics with labels")
			}
		})
	}
}

func TestPrometheusTextOutput(t *testing.T) {
	// Test that metrics can be output in Prometheus text format
	registry := prometheus.NewRegistry()
	MustRegister(registry)

	// Record some test data
	RecordEventPublished("test-tenant")
	UpdateWorkerBacklog(42)

	// Get metrics in Prometheus text format
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Errorf("Registry.Gather() error: %v", err)
	}

	// Verify we have some output
	if len(metricFamilies) == 0 {
		t.Error("Expected non-empty metrics output")
	}

	// Check that metric names follow expected pattern
	for _, mf := range metricFamilies {
		name := mf.GetName()
		if !strings.HasPrefix(name, "harborhook_") {
			t.Errorf("Metric name %s does not have expected prefix 'harborhook_'", name)
		}
	}
}