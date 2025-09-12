package delivery

type Task struct {
	DeliveryID   string            `json:"delivery_id"`
	EventID      string            `json:"event_id"`
	TenantID     string            `json:"tenant_id"`
	EndpointID   string            `json:"endpoint_id"`
	EndpointURL  string            `json:"endpoint_url"`
	EventType    string            `json:"event_type"`
	Payload      map[string]any    `json:"payload"`
	Attempt      int               `json:"attempt"`
	PublishedAt  string            `json:"published_at"` // RFC3339
	TraceHeaders map[string]string `json:"trace_headers,omitempty"` // OTel trace propagation headers
}
