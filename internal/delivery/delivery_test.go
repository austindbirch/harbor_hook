package delivery

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewDeadLetter(t *testing.T) {
	tests := []struct {
		name       string
		task       Task
		attempt    int
		httpStatus int
		lastErr    string
		reason     string
	}{
		{
			name: "complete dead letter creation",
			task: Task{
				DeliveryID:  "delivery-123",
				EventID:     "event-456",
				TenantID:    "tenant-789",
				EndpointID:  "endpoint-abc",
				EndpointURL: "https://example.com/webhook",
				EventType:   "user.created",
				Payload:     map[string]any{"user_id": 123, "email": "test@example.com"},
				Attempt:     3,
				PublishedAt: "2023-01-01T12:00:00Z",
				TraceHeaders: map[string]string{
					"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
				},
			},
			attempt:    5,
			httpStatus: 500,
			lastErr:    "connection timeout",
			reason:     "max retries exceeded",
		},
		{
			name: "minimal dead letter creation",
			task: Task{
				DeliveryID: "delivery-minimal",
				EventID:    "event-minimal",
			},
			attempt:    1,
			httpStatus: 404,
			lastErr:    "not found",
			reason:     "endpoint not found",
		},
		{
			name: "empty error and reason",
			task: Task{
				DeliveryID: "delivery-empty",
			},
			attempt:    2,
			httpStatus: 0,
			lastErr:    "",
			reason:     "",
		},
		{
			name: "zero attempt count",
			task: Task{
				DeliveryID: "delivery-zero",
			},
			attempt:    0,
			httpStatus: 200,
			lastErr:    "success but dlq'd for testing",
			reason:     "test case",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now()
			dl := NewDeadLetter(tt.task, tt.attempt, tt.httpStatus, tt.lastErr, tt.reason)
			after := time.Now()

			// Test constant fields
			if dl.Type != DLQType {
				t.Errorf("NewDeadLetter() Type = %q, want %q", dl.Type, DLQType)
			}
			if dl.Version != "v1" {
				t.Errorf("NewDeadLetter() Version = %q, want %q", dl.Version, "v1")
			}

			// Test parameter assignments
			if dl.Reason != tt.reason {
				t.Errorf("NewDeadLetter() Reason = %q, want %q", dl.Reason, tt.reason)
			}
			if dl.Attempt != tt.attempt {
				t.Errorf("NewDeadLetter() Attempt = %d, want %d", dl.Attempt, tt.attempt)
			}
			if dl.HTTPStatus != tt.httpStatus {
				t.Errorf("NewDeadLetter() HTTPStatus = %d, want %d", dl.HTTPStatus, tt.httpStatus)
			}
			if dl.LastError != tt.lastErr {
				t.Errorf("NewDeadLetter() LastError = %q, want %q", dl.LastError, tt.lastErr)
			}

			// Test task assignment (deep comparison)
			if dl.Task.DeliveryID != tt.task.DeliveryID {
				t.Errorf("NewDeadLetter() Task.DeliveryID = %q, want %q", dl.Task.DeliveryID, tt.task.DeliveryID)
			}
			if dl.Task.EventID != tt.task.EventID {
				t.Errorf("NewDeadLetter() Task.EventID = %q, want %q", dl.Task.EventID, tt.task.EventID)
			}

			// Test timestamp format and timing
			parsedTime, err := time.Parse(time.RFC3339Nano, dl.At)
			if err != nil {
				t.Errorf("NewDeadLetter() At timestamp parse error: %v", err)
			}
			if parsedTime.Before(before) || parsedTime.After(after) {
				t.Errorf("NewDeadLetter() At timestamp %v not between %v and %v", parsedTime, before, after)
			}
		})
	}
}

func TestDeadLetterJSONSerialization(t *testing.T) {
	tests := []struct {
		name       string
		deadLetter DeadLetter
	}{
		{
			name: "full dead letter serialization",
			deadLetter: DeadLetter{
				Type:       DLQType,
				Version:    "v1",
				At:         "2023-01-01T12:00:00.123456789Z",
				Reason:     "max retries exceeded",
				Attempt:    5,
				HTTPStatus: 500,
				LastError:  "connection timeout",
				Task: Task{
					DeliveryID:   "delivery-123",
					EventID:      "event-456",
					TenantID:     "tenant-789",
					EndpointID:   "endpoint-abc",
					EndpointURL:  "https://example.com/webhook",
					EventType:    "user.created",
					Payload:      map[string]any{"user_id": 123, "active": true},
					Attempt:      3,
					PublishedAt:  "2023-01-01T11:00:00Z",
					TraceHeaders: map[string]string{"traceparent": "trace-123"},
				},
			},
		},
		{
			name: "minimal dead letter serialization",
			deadLetter: DeadLetter{
				Type:    DLQType,
				Version: "v1",
				At:      "2023-01-01T12:00:00Z",
				Reason:  "test",
				Attempt: 1,
				Task: Task{
					DeliveryID: "delivery-minimal",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			jsonData, err := json.Marshal(tt.deadLetter)
			if err != nil {
				t.Errorf("DeadLetter JSON marshal error: %v", err)
			}

			// Test unmarshaling
			var unmarshaled DeadLetter
			err = json.Unmarshal(jsonData, &unmarshaled)
			if err != nil {
				t.Errorf("DeadLetter JSON unmarshal error: %v", err)
			}

			// Verify round-trip integrity
			if unmarshaled.Type != tt.deadLetter.Type {
				t.Errorf("JSON round-trip Type mismatch: got %q, want %q", unmarshaled.Type, tt.deadLetter.Type)
			}
			if unmarshaled.Version != tt.deadLetter.Version {
				t.Errorf("JSON round-trip Version mismatch: got %q, want %q", unmarshaled.Version, tt.deadLetter.Version)
			}
			if unmarshaled.At != tt.deadLetter.At {
				t.Errorf("JSON round-trip At mismatch: got %q, want %q", unmarshaled.At, tt.deadLetter.At)
			}
			if unmarshaled.Reason != tt.deadLetter.Reason {
				t.Errorf("JSON round-trip Reason mismatch: got %q, want %q", unmarshaled.Reason, tt.deadLetter.Reason)
			}
			if unmarshaled.Attempt != tt.deadLetter.Attempt {
				t.Errorf("JSON round-trip Attempt mismatch: got %d, want %d", unmarshaled.Attempt, tt.deadLetter.Attempt)
			}
			if unmarshaled.HTTPStatus != tt.deadLetter.HTTPStatus {
				t.Errorf("JSON round-trip HTTPStatus mismatch: got %d, want %d", unmarshaled.HTTPStatus, tt.deadLetter.HTTPStatus)
			}
			if unmarshaled.Task.DeliveryID != tt.deadLetter.Task.DeliveryID {
				t.Errorf("JSON round-trip Task.DeliveryID mismatch: got %q, want %q", unmarshaled.Task.DeliveryID, tt.deadLetter.Task.DeliveryID)
			}
		})
	}
}

func TestTaskJSONSerialization(t *testing.T) {
	tests := []struct {
		name string
		task Task
	}{
		{
			name: "complete task serialization",
			task: Task{
				DeliveryID:   "delivery-123",
				EventID:      "event-456",
				TenantID:     "tenant-789",
				EndpointID:   "endpoint-abc",
				EndpointURL:  "https://example.com/webhook",
				EventType:    "user.created",
				Payload:      map[string]any{"user_id": 123, "data": map[string]any{"nested": true}},
				Attempt:      3,
				PublishedAt:  "2023-01-01T12:00:00Z",
				TraceHeaders: map[string]string{"traceparent": "trace-123", "tracestate": "state-456"},
			},
		},
		{
			name: "minimal task serialization",
			task: Task{
				DeliveryID: "delivery-minimal",
				EventID:    "event-minimal",
			},
		},
		{
			name: "task with nil payload and headers",
			task: Task{
				DeliveryID:   "delivery-nil",
				EventID:      "event-nil",
				Payload:      nil,
				TraceHeaders: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			jsonData, err := json.Marshal(tt.task)
			if err != nil {
				t.Errorf("Task JSON marshal error: %v", err)
			}

			// Test unmarshaling
			var unmarshaled Task
			err = json.Unmarshal(jsonData, &unmarshaled)
			if err != nil {
				t.Errorf("Task JSON unmarshal error: %v", err)
			}

			// Verify round-trip integrity for key fields
			if unmarshaled.DeliveryID != tt.task.DeliveryID {
				t.Errorf("JSON round-trip DeliveryID mismatch: got %q, want %q", unmarshaled.DeliveryID, tt.task.DeliveryID)
			}
			if unmarshaled.EventID != tt.task.EventID {
				t.Errorf("JSON round-trip EventID mismatch: got %q, want %q", unmarshaled.EventID, tt.task.EventID)
			}
			if unmarshaled.TenantID != tt.task.TenantID {
				t.Errorf("JSON round-trip TenantID mismatch: got %q, want %q", unmarshaled.TenantID, tt.task.TenantID)
			}
			if unmarshaled.Attempt != tt.task.Attempt {
				t.Errorf("JSON round-trip Attempt mismatch: got %d, want %d", unmarshaled.Attempt, tt.task.Attempt)
			}
		})
	}
}

func TestDLQTypeConstant(t *testing.T) {
	expected := "delivery.dlq"
	if DLQType != expected {
		t.Errorf("DLQType constant = %q, want %q", DLQType, expected)
	}
}