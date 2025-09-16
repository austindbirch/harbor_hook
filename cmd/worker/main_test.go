package main

// TODO: Add tests that require more setup and scaffolding:
// - Integration tests with real NSQ consumer/producer setup
// - Database interaction testing with pgxpool connections
// - HTTP client delivery testing with test servers
// - Full webhook delivery workflow testing (success/failure/retry/DLQ)
// - NSQ message handler testing with actual message processing
// - Backlog monitoring integration testing
// - Signal handling and graceful shutdown testing
// - HMAC signature generation and verification testing
// - Tracing and metrics collection testing
// - End-to-end worker service testing with real infrastructure
// - Retry configuration and backoff schedule testing
// - DLQ publishing and database operations testing
// - Error classification and failure reason testing

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func TestParseEnvInt(t *testing.T) {
	// Save original environment
	originalValue := os.Getenv("TEST_INT_VAR")
	defer func() {
		if originalValue == "" {
			os.Unsetenv("TEST_INT_VAR")
		} else {
			os.Setenv("TEST_INT_VAR", originalValue)
		}
	}()

	tests := []struct {
		name     string
		envVar   string
		envValue string
		def      int
		expected int
	}{
		{
			name:     "valid integer",
			envVar:   "TEST_INT_VAR",
			envValue: "42",
			def:      10,
			expected: 42,
		},
		{
			name:     "invalid integer",
			envVar:   "TEST_INT_VAR",
			envValue: "not-an-int",
			def:      10,
			expected: 10,
		},
		{
			name:     "empty string",
			envVar:   "TEST_INT_VAR",
			envValue: "",
			def:      10,
			expected: 10,
		},
		{
			name:     "negative integer",
			envVar:   "TEST_INT_VAR",
			envValue: "-5",
			def:      10,
			expected: -5,
		},
		{
			name:     "zero",
			envVar:   "TEST_INT_VAR",
			envValue: "0",
			def:      10,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv(tt.envVar)
			} else {
				os.Setenv(tt.envVar, tt.envValue)
			}

			result := parseEnvInt(tt.envVar, tt.def)
			if result != tt.expected {
				t.Errorf("parseEnvInt(%q, %d) = %d, want %d", tt.envVar, tt.def, result, tt.expected)
			}
		})
	}
}

func TestParseEnvFloat(t *testing.T) {
	// Save original environment
	originalValue := os.Getenv("TEST_FLOAT_VAR")
	defer func() {
		if originalValue == "" {
			os.Unsetenv("TEST_FLOAT_VAR")
		} else {
			os.Setenv("TEST_FLOAT_VAR", originalValue)
		}
	}()

	tests := []struct {
		name     string
		envVar   string
		envValue string
		def      float64
		expected float64
	}{
		{
			name:     "valid float",
			envVar:   "TEST_FLOAT_VAR",
			envValue: "3.14",
			def:      1.0,
			expected: 3.14,
		},
		{
			name:     "valid integer as float",
			envVar:   "TEST_FLOAT_VAR",
			envValue: "42",
			def:      1.0,
			expected: 42.0,
		},
		{
			name:     "invalid float",
			envVar:   "TEST_FLOAT_VAR",
			envValue: "not-a-float",
			def:      1.0,
			expected: 1.0,
		},
		{
			name:     "empty string",
			envVar:   "TEST_FLOAT_VAR",
			envValue: "",
			def:      1.0,
			expected: 1.0,
		},
		{
			name:     "negative float",
			envVar:   "TEST_FLOAT_VAR",
			envValue: "-2.5",
			def:      1.0,
			expected: -2.5,
		},
		{
			name:     "zero",
			envVar:   "TEST_FLOAT_VAR",
			envValue: "0.0",
			def:      1.0,
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv(tt.envVar)
			} else {
				os.Setenv(tt.envVar, tt.envValue)
			}

			result := parseEnvFloat(tt.envVar, tt.def)
			if result != tt.expected {
				t.Errorf("parseEnvFloat(%q, %f) = %f, want %f", tt.envVar, tt.def, result, tt.expected)
			}
		})
	}
}

func TestReadRetryCfg(t *testing.T) {
	// Save original environment variables
	originalEnvVars := map[string]string{
		"MAX_ATTEMPTS":       os.Getenv("MAX_ATTEMPTS"),
		"BACKOFF_SCHEDULE":   os.Getenv("BACKOFF_SCHEDULE"),
		"BACKOFF_JITTER_PCT": os.Getenv("BACKOFF_JITTER_PCT"),
		"PUBLISH_DLQ_TOPIC":  os.Getenv("PUBLISH_DLQ_TOPIC"),
	}

	// Restore environment after test
	defer func() {
		for k, v := range originalEnvVars {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(t *testing.T, cfg retryCfg)
	}{
		{
			name:    "default configuration",
			envVars: map[string]string{
				// No environment variables set - should use defaults
			},
			validate: func(t *testing.T, cfg retryCfg) {
				if cfg.maxAttempts != 6 {
					t.Errorf("Expected maxAttempts 6, got %d", cfg.maxAttempts)
				}
				if cfg.jitterPct != 0.25 {
					t.Errorf("Expected jitterPct 0.25, got %f", cfg.jitterPct)
				}
				if cfg.publishDLQ != false {
					t.Errorf("Expected publishDLQ false, got %v", cfg.publishDLQ)
				}
				expectedSchedule := []time.Duration{
					time.Second,
					4 * time.Second,
					16 * time.Second,
					time.Minute,
					4 * time.Minute,
					10 * time.Minute,
				}
				if len(cfg.backoff) != len(expectedSchedule) {
					t.Errorf("Expected backoff schedule length %d, got %d", len(expectedSchedule), len(cfg.backoff))
					return
				}
				for i, expected := range expectedSchedule {
					if cfg.backoff[i] != expected {
						t.Errorf("Expected backoff[%d] = %v, got %v", i, expected, cfg.backoff[i])
					}
				}
			},
		},
		{
			name: "custom configuration",
			envVars: map[string]string{
				"MAX_ATTEMPTS":       "3",
				"BACKOFF_SCHEDULE":   "2s,8s,30s",
				"BACKOFF_JITTER_PCT": "0.1",
				"PUBLISH_DLQ_TOPIC":  "true",
			},
			validate: func(t *testing.T, cfg retryCfg) {
				if cfg.maxAttempts != 3 {
					t.Errorf("Expected maxAttempts 3, got %d", cfg.maxAttempts)
				}
				if cfg.jitterPct != 0.1 {
					t.Errorf("Expected jitterPct 0.1, got %f", cfg.jitterPct)
				}
				if cfg.publishDLQ != true {
					t.Errorf("Expected publishDLQ true, got %v", cfg.publishDLQ)
				}
				expectedSchedule := []time.Duration{
					2 * time.Second,
					8 * time.Second,
					30 * time.Second,
				}
				if len(cfg.backoff) != len(expectedSchedule) {
					t.Errorf("Expected backoff schedule length %d, got %d", len(expectedSchedule), len(cfg.backoff))
					return
				}
				for i, expected := range expectedSchedule {
					if cfg.backoff[i] != expected {
						t.Errorf("Expected backoff[%d] = %v, got %v", i, expected, cfg.backoff[i])
					}
				}
			},
		},
		{
			name: "invalid backoff schedule falls back to default",
			envVars: map[string]string{
				"BACKOFF_SCHEDULE": "invalid,also-invalid",
			},
			validate: func(t *testing.T, cfg retryCfg) {
				// Should fall back to default schedule
				expectedSchedule := []time.Duration{
					time.Second,
					4 * time.Second,
					16 * time.Second,
					time.Minute,
					4 * time.Minute,
					10 * time.Minute,
				}
				if len(cfg.backoff) != len(expectedSchedule) {
					t.Errorf("Expected fallback to default schedule length %d, got %d", len(expectedSchedule), len(cfg.backoff))
				}
			},
		},
		{
			name: "DLQ topic case insensitive",
			envVars: map[string]string{
				"PUBLISH_DLQ_TOPIC": "TRUE",
			},
			validate: func(t *testing.T, cfg retryCfg) {
				if cfg.publishDLQ != true {
					t.Errorf("Expected publishDLQ true for 'TRUE', got %v", cfg.publishDLQ)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			os.Unsetenv("MAX_ATTEMPTS")
			os.Unsetenv("BACKOFF_SCHEDULE")
			os.Unsetenv("BACKOFF_JITTER_PCT")
			os.Unsetenv("PUBLISH_DLQ_TOPIC")

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := readRetryCfg()
			tt.validate(t, cfg)
		})
	}
}

func TestErrString(t *testing.T) {
	// Test nil error
	t.Run("nil error", func(t *testing.T) {
		result := errString(nil)
		if result != "" {
			t.Errorf("errString(nil) = %q, want empty string", result)
		}
	})

	// Test with a real error
	t.Run("real error", func(t *testing.T) {
		testErr := strconv.ErrSyntax
		result := errString(testErr)
		if result != testErr.Error() {
			t.Errorf("errString(%v) = %q, want %q", testErr, result, testErr.Error())
		}
	})
}

func TestComputeDelay(t *testing.T) {
	schedule := []time.Duration{
		1 * time.Second,
		4 * time.Second,
		16 * time.Second,
		1 * time.Minute,
	}

	tests := []struct {
		name      string
		attempt   int
		schedule  []time.Duration
		jitterPct float64
		validate  func(t *testing.T, result time.Duration, baseExpected time.Duration)
	}{
		{
			name:      "first attempt",
			attempt:   1,
			schedule:  schedule,
			jitterPct: 0.0, // No jitter for predictable testing
			validate: func(t *testing.T, result time.Duration, baseExpected time.Duration) {
				if result != baseExpected {
					t.Errorf("Expected delay %v, got %v", baseExpected, result)
				}
			},
		},
		{
			name:      "attempt within schedule",
			attempt:   3,
			schedule:  schedule,
			jitterPct: 0.0,
			validate: func(t *testing.T, result time.Duration, baseExpected time.Duration) {
				if result != baseExpected {
					t.Errorf("Expected delay %v, got %v", baseExpected, result)
				}
			},
		},
		{
			name:      "attempt beyond schedule",
			attempt:   10,
			schedule:  schedule,
			jitterPct: 0.0,
			validate: func(t *testing.T, result time.Duration, baseExpected time.Duration) {
				// Should use the last item in schedule
				if result != baseExpected {
					t.Errorf("Expected delay %v (last in schedule), got %v", baseExpected, result)
				}
			},
		},
		{
			name:      "zero attempt",
			attempt:   0,
			schedule:  schedule,
			jitterPct: 0.0,
			validate: func(t *testing.T, result time.Duration, baseExpected time.Duration) {
				// Should use first item in schedule
				if result != baseExpected {
					t.Errorf("Expected delay %v (first in schedule), got %v", baseExpected, result)
				}
			},
		},
		{
			name:      "negative attempt",
			attempt:   -1,
			schedule:  schedule,
			jitterPct: 0.0,
			validate: func(t *testing.T, result time.Duration, baseExpected time.Duration) {
				// Should use first item in schedule
				if result != baseExpected {
					t.Errorf("Expected delay %v (first in schedule), got %v", baseExpected, result)
				}
			},
		},
		{
			name:      "with jitter",
			attempt:   2,
			schedule:  schedule,
			jitterPct: 0.5,
			validate: func(t *testing.T, result time.Duration, baseExpected time.Duration) {
				// With jitter, result should be within reasonable bounds
				minExpected := time.Duration(float64(baseExpected) * 0.1) // Minimum jitter bound
				maxExpected := time.Duration(float64(baseExpected) * 1.5) // Maximum jitter bound
				if result < minExpected || result > maxExpected {
					t.Errorf("Expected delay between %v and %v, got %v", minExpected, maxExpected, result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeDelay(tt.attempt, tt.schedule, tt.jitterPct)

			// Determine expected base value
			idx := tt.attempt - 1
			if idx < 0 {
				idx = 0
			}
			if idx >= len(tt.schedule) {
				idx = len(tt.schedule) - 1
			}
			baseExpected := tt.schedule[idx]

			tt.validate(t, result, baseExpected)
		})
	}
}

func TestClassifyReason(t *testing.T) {
	// Test with actual error types
	t.Run("timeout error", func(t *testing.T) {
		// Create an error that contains "timeout"
		err := &timeoutError{message: "request timeout"}
		result := classifyReason(err, 0)
		if result != "timeout" {
			t.Errorf("Expected 'timeout', got %q", result)
		}
	})

	t.Run("connection refused error", func(t *testing.T) {
		// Create an error that contains "connection refused"
		err := &connectionError{message: "connection refused"}
		result := classifyReason(err, 0)
		if result != "connection_refused" {
			t.Errorf("Expected 'connection_refused', got %q", result)
		}
	})

	t.Run("DNS error", func(t *testing.T) {
		err := &dnsError{message: "no such host"}
		result := classifyReason(err, 0)
		if result != "dns_error" {
			t.Errorf("Expected 'dns_error', got %q", result)
		}
	})

	t.Run("generic network error", func(t *testing.T) {
		err := &networkError{message: "network unreachable"}
		result := classifyReason(err, 0)
		if result != "network" {
			t.Errorf("Expected 'network', got %q", result)
		}
	})

	// Test HTTP status codes
	tests := []struct {
		name     string
		doErr    error
		status   int
		expected string
	}{
		{
			name:     "HTTP 500",
			doErr:    nil,
			status:   500,
			expected: "http_5xx",
		},
		{
			name:     "HTTP 503",
			doErr:    nil,
			status:   503,
			expected: "http_5xx",
		},
		{
			name:     "HTTP 429",
			doErr:    nil,
			status:   429,
			expected: "http_429",
		},
		{
			name:     "HTTP 400",
			doErr:    nil,
			status:   400,
			expected: "http_4xx",
		},
		{
			name:     "HTTP 404",
			doErr:    nil,
			status:   404,
			expected: "http_4xx",
		},
		{
			name:     "HTTP 200",
			doErr:    nil,
			status:   200,
			expected: "other",
		},
		{
			name:     "HTTP 300",
			doErr:    nil,
			status:   300,
			expected: "other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyReason(tt.doErr, tt.status)
			if result != tt.expected {
				t.Errorf("classifyReason(%v, %d) = %q, want %q", tt.doErr, tt.status, result, tt.expected)
			}
		})
	}
}

// Test error types for classifyReason testing
type timeoutError struct {
	message string
}

func (e *timeoutError) Error() string {
	return e.message
}

type connectionError struct {
	message string
}

func (e *connectionError) Error() string {
	return e.message
}

type dnsError struct {
	message string
}

func (e *dnsError) Error() string {
	return e.message
}

type networkError struct {
	message string
}

func (e *networkError) Error() string {
	return e.message
}

func TestConstants(t *testing.T) {
	// Test that constants are defined correctly
	if topic != "deliveries" {
		t.Errorf("Expected topic 'deliveries', got %q", topic)
	}
	if channel != "workers" {
		t.Errorf("Expected channel 'workers', got %q", channel)
	}
	if dlqTopic != "deliveries_dlq" {
		t.Errorf("Expected dlqTopic 'deliveries_dlq', got %q", dlqTopic)
	}
	if sigHeader != "X-HarborHook-Signature" {
		t.Errorf("Expected sigHeader 'X-HarborHook-Signature', got %q", sigHeader)
	}
	if tsHeader != "X-HarborHook-Timestamp" {
		t.Errorf("Expected tsHeader 'X-HarborHook-Timestamp', got %q", tsHeader)
	}
}

func TestRetryCfgStruct(t *testing.T) {
	// Test that the retryCfg struct can be created and used
	cfg := retryCfg{
		maxAttempts: 5,
		backoff:     []time.Duration{time.Second, time.Minute},
		jitterPct:   0.1,
		publishDLQ:  true,
	}

	if cfg.maxAttempts != 5 {
		t.Errorf("Expected maxAttempts 5, got %d", cfg.maxAttempts)
	}
	if len(cfg.backoff) != 2 {
		t.Errorf("Expected backoff length 2, got %d", len(cfg.backoff))
	}
	if cfg.jitterPct != 0.1 {
		t.Errorf("Expected jitterPct 0.1, got %f", cfg.jitterPct)
	}
	if cfg.publishDLQ != true {
		t.Errorf("Expected publishDLQ true, got %v", cfg.publishDLQ)
	}
}
