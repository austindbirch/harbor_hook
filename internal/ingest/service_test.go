package ingest

// TODO: Add tests that require more setup and scaffolding:
// - Integration tests with real pgxpool.Pool database connections
// - NSQ producer integration tests for message publishing
// - End-to-end workflow tests (CreateEndpoint -> CreateSubscription -> PublishEvent -> GetDeliveryStatus)
// - Database transaction testing for atomic operations
// - Concurrent access testing for thread safety
// - Error recovery testing with database connection failures
// - Message delivery retry logic testing
// - Dead letter queue integration testing

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
)

func TestNewServer(t *testing.T) {
	// Test with nil values since we can't easily mock the external dependencies
	server := NewServer(nil, nil)

	if server == nil {
		t.Error("NewServer() returned nil")
	}
}

func TestServer_Ping(t *testing.T) {
	server := &Server{}

	tests := []struct {
		name string
		req  *webhookv1.PingRequest
	}{
		{
			name: "basic ping",
			req:  &webhookv1.PingRequest{},
		},
		{
			name: "ping with nil request",
			req:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := server.Ping(context.Background(), tt.req)

			if err != nil {
				t.Errorf("Ping() unexpected error: %v", err)
			}
			if resp == nil {
				t.Error("Ping() returned nil response")
			}
			if resp != nil && resp.Message != "pong" {
				t.Errorf("Ping() message = %q, want %q", resp.Message, "pong")
			}
		})
	}
}

func TestGenerateSecret(t *testing.T) {
	tests := []struct {
		name        string
		length      int
		expectError bool
	}{
		{
			name:        "valid length 16",
			length:      16,
			expectError: false,
		},
		{
			name:        "valid length 32",
			length:      32,
			expectError: false,
		},
		{
			name:        "valid length 64",
			length:      64,
			expectError: false,
		},
		{
			name:        "zero length",
			length:      0,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, err := generateSecret(tt.length)

			if tt.expectError {
				if err == nil {
					t.Error("generateSecret() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("generateSecret() unexpected error: %v", err)
				}
				if tt.length > 0 && secret == "" {
					t.Error("generateSecret() returned empty secret for non-zero length")
				}
				// Base64 URL encoding should produce reasonable output length
				if tt.length > 0 && len(secret) < tt.length/2 {
					t.Errorf("generateSecret() secret length = %d, expected at least %d", len(secret), tt.length/2)
				}
			}
		})
	}
}

func TestServer_CreateEndpoint_Validation(t *testing.T) {
	tests := []struct {
		name        string
		request     *webhookv1.CreateEndpointRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "missing tenant_id",
			request: &webhookv1.CreateEndpointRequest{
				Url:    "https://example.com/webhook",
				Secret: "test-secret",
			},
			expectError: true,
			errorMsg:    "tenant_id and url are required",
		},
		{
			name: "missing url",
			request: &webhookv1.CreateEndpointRequest{
				TenantId: "tenant-123",
				Secret:   "test-secret",
			},
			expectError: true,
			errorMsg:    "tenant_id and url are required",
		},
		{
			name: "invalid url format",
			request: &webhookv1.CreateEndpointRequest{
				TenantId: "tenant-123",
				Url:      "not-a-valid-url",
				Secret:   "test-secret",
			},
			expectError: true,
			errorMsg:    "invalid url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{} // No database connection, will fail on DB operations

			_, err := server.CreateEndpoint(context.Background(), tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("CreateEndpoint() expected error but got none")
				}
				if tt.errorMsg != "" && err != nil {
					if !contains(err.Error(), tt.errorMsg) {
						t.Errorf("CreateEndpoint() error = %q, want to contain %q", err.Error(), tt.errorMsg)
					}
				}
			}
		})
	}
}

func TestServer_CreateSubscription_Validation(t *testing.T) {
	tests := []struct {
		name        string
		request     *webhookv1.CreateSubscriptionRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "missing tenant_id",
			request: &webhookv1.CreateSubscriptionRequest{
				EventType:  "user.created",
				EndpointId: "endpoint-abc",
			},
			expectError: true,
			errorMsg:    "tenant_id, event_type, and endpoint_id are required",
		},
		{
			name: "missing event_type",
			request: &webhookv1.CreateSubscriptionRequest{
				TenantId:   "tenant-123",
				EndpointId: "endpoint-abc",
			},
			expectError: true,
			errorMsg:    "tenant_id, event_type, and endpoint_id are required",
		},
		{
			name: "missing endpoint_id",
			request: &webhookv1.CreateSubscriptionRequest{
				TenantId:  "tenant-123",
				EventType: "user.created",
			},
			expectError: true,
			errorMsg:    "tenant_id, event_type, and endpoint_id are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{} // No database connection

			_, err := server.CreateSubscription(context.Background(), tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("CreateSubscription() expected error but got none")
				}
				if tt.errorMsg != "" && err != nil {
					if err.Error() != tt.errorMsg {
						t.Errorf("CreateSubscription() error = %q, want %q", err.Error(), tt.errorMsg)
					}
				}
			}
		})
	}
}

func TestServer_PublishEvent_Validation(t *testing.T) {
	// Create test payload
	payload, _ := structpb.NewStruct(map[string]any{
		"user_id": "123",
		"email":   "test@example.com",
	})

	tests := []struct {
		name        string
		request     *webhookv1.PublishEventRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "missing tenant_id",
			request: &webhookv1.PublishEventRequest{
				EventType: "user.created",
				Payload:   payload,
			},
			expectError: true,
			errorMsg:    "tenant_id, event_type, and payload are required",
		},
		{
			name: "missing event_type",
			request: &webhookv1.PublishEventRequest{
				TenantId: "tenant-123",
				Payload:  payload,
			},
			expectError: true,
			errorMsg:    "tenant_id, event_type, and payload are required",
		},
		{
			name: "missing payload",
			request: &webhookv1.PublishEventRequest{
				TenantId:  "tenant-123",
				EventType: "user.created",
			},
			expectError: true,
			errorMsg:    "tenant_id, event_type, and payload are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{}

			_, err := server.PublishEvent(context.Background(), tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("PublishEvent() expected error but got none")
				}
				if tt.errorMsg != "" && err != nil {
					if err.Error() != tt.errorMsg {
						t.Errorf("PublishEvent() error = %q, want %q", err.Error(), tt.errorMsg)
					}
				}
			}
		})
	}
}

func TestServer_GetDeliveryStatus_Validation(t *testing.T) {
	tests := []struct {
		name        string
		request     *webhookv1.GetDeliveryStatusRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "missing event_id",
			request: &webhookv1.GetDeliveryStatusRequest{
				EndpointId: "endpoint-123",
			},
			expectError: true,
			errorMsg:    "event_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{}

			_, err := server.GetDeliveryStatus(context.Background(), tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("GetDeliveryStatus() expected error but got none")
				}
				if tt.errorMsg != "" && err != nil {
					if err.Error() != tt.errorMsg {
						t.Errorf("GetDeliveryStatus() error = %q, want %q", err.Error(), tt.errorMsg)
					}
				}
			}
		})
	}
}

func TestServer_ReplayDelivery_Validation(t *testing.T) {
	tests := []struct {
		name        string
		request     *webhookv1.ReplayDeliveryRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "missing delivery_id",
			request: &webhookv1.ReplayDeliveryRequest{
				Reason: "test replay",
			},
			expectError: true,
			errorMsg:    "delivery_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{}

			_, err := server.ReplayDelivery(context.Background(), tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("ReplayDelivery() expected error but got none")
				}
				if tt.errorMsg != "" && err != nil {
					if err.Error() != tt.errorMsg {
						t.Errorf("ReplayDelivery() error = %q, want %q", err.Error(), tt.errorMsg)
					}
				}
			}
		})
	}
}

// TestServer_ListDLQ is omitted because ListDLQ has no validation logic to test
// It immediately builds and executes a database query with optional filters

func TestHelperFunctions(t *testing.T) {
	t.Run("nullStr", func(t *testing.T) {
		tests := []struct {
			name     string
			input    sql.NullString
			expected string
		}{
			{
				name:     "valid string",
				input:    sql.NullString{String: "test", Valid: true},
				expected: "test",
			},
			{
				name:     "null string",
				input:    sql.NullString{String: "", Valid: false},
				expected: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := nullStr(tt.input)
				if result != tt.expected {
					t.Errorf("nullStr() = %q, want %q", result, tt.expected)
				}
			})
		}
	})

	t.Run("nullI32", func(t *testing.T) {
		tests := []struct {
			name     string
			input    sql.NullInt32
			expected int32
		}{
			{
				name:     "valid int32",
				input:    sql.NullInt32{Int32: 200, Valid: true},
				expected: 200,
			},
			{
				name:     "null int32",
				input:    sql.NullInt32{Int32: 0, Valid: false},
				expected: 0,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := nullI32(tt.input)
				if result != tt.expected {
					t.Errorf("nullI32() = %d, want %d", result, tt.expected)
				}
			})
		}
	})

	t.Run("toTS", func(t *testing.T) {
		testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		tests := []struct {
			name     string
			input    sql.NullTime
			expected *timestamppb.Timestamp
		}{
			{
				name:     "valid time",
				input:    sql.NullTime{Time: testTime, Valid: true},
				expected: timestamppb.New(testTime),
			},
			{
				name:     "null time",
				input:    sql.NullTime{Time: time.Time{}, Valid: false},
				expected: nil,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := toTS(tt.input)
				if (result == nil) != (tt.expected == nil) {
					t.Errorf("toTS() nil comparison failed: got %v, want %v", result == nil, tt.expected == nil)
				}
				if result != nil && tt.expected != nil {
					if !result.AsTime().Equal(tt.expected.AsTime()) {
						t.Errorf("toTS() = %v, want %v", result.AsTime(), tt.expected.AsTime())
					}
				}
			})
		}
	})

	t.Run("mapStatus", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected webhookv1.DeliveryAttemptStatus
		}{
			{
				name:     "queued status",
				input:    "queued",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_QUEUED,
			},
			{
				name:     "pending status",
				input:    "pending",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_QUEUED,
			},
			{
				name:     "inflight status",
				input:    "inflight",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_IN_FLIGHT,
			},
			{
				name:     "delivered status",
				input:    "delivered",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_DELIVERED,
			},
			{
				name:     "ok status",
				input:    "ok",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_DELIVERED,
			},
			{
				name:     "failed status",
				input:    "failed",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_FAILED,
			},
			{
				name:     "dead status",
				input:    "dead",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_DEAD_LETTERED,
			},
			{
				name:     "unknown status",
				input:    "unknown",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_UNSPECIFIED,
			},
			{
				name:     "empty status",
				input:    "",
				expected: webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_UNSPECIFIED,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := mapStatus(tt.input)
				if result != tt.expected {
					t.Errorf("mapStatus(%q) = %v, want %v", tt.input, result, tt.expected)
				}
			})
		}
	})
}

func TestDeliveriesTopicConstant(t *testing.T) {
	expected := "deliveries"
	if deliveriesTopic != expected {
		t.Errorf("deliveriesTopic constant = %q, want %q", deliveriesTopic, expected)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			len(s) > 2*len(substr) && indexOf(s, substr) != -1)))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
