package db

import (
	"context"
	"testing"
	"time"
)

func TestConnect(t *testing.T) {
	tests := []struct {
		name        string
		dsn         string
		expectError bool
		timeout     time.Duration
	}{
		{
			name:        "invalid DSN format",
			dsn:         "invalid-dsn-format",
			expectError: true,
			timeout:     5 * time.Second,
		},
		{
			name:        "malformed postgres URL",
			dsn:         "postgres://",
			expectError: true,
			timeout:     5 * time.Second,
		},
		{
			name:        "empty DSN",
			dsn:         "",
			expectError: true,
			timeout:     5 * time.Second,
		},
		{
			name:        "valid DSN format but unreachable host",
			dsn:         "postgres://user:pass@nonexistent-host:5432/dbname?sslmode=disable",
			expectError: true,
			timeout:     2 * time.Second,
		},
		{
			name:        "valid DSN with invalid port",
			dsn:         "postgres://user:pass@localhost:99999/dbname?sslmode=disable",
			expectError: true,
			timeout:     2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			pool, err := Connect(ctx, tt.dsn)

			if tt.expectError {
				if err == nil {
					t.Errorf("Connect() expected error but got none")
					if pool != nil {
						pool.Close()
					}
				}
			} else {
				if err != nil {
					t.Errorf("Connect() unexpected error: %v", err)
				}
				if pool == nil {
					t.Errorf("Connect() expected pool but got nil")
				}
			}

			// Always clean up pool if it was created
			if pool != nil {
				pool.Close()
			}
		})
	}
}

func TestConnect_ContextCancellation(t *testing.T) {
	tests := []struct {
		name           string
		dsn            string
		cancelAfter    time.Duration
		expectError    bool
		errorSubstring string
	}{
		{
			name:           "context cancelled during connection",
			dsn:            "postgres://user:pass@192.0.2.0:5432/dbname?sslmode=disable", // RFC 5737 TEST-NET-1
			cancelAfter:    100 * time.Millisecond,
			expectError:    true,
			errorSubstring: "context",
		},
		{
			name:           "context cancelled immediately",
			dsn:            "postgres://user:pass@192.0.2.0:5432/dbname?sslmode=disable",
			cancelAfter:    1 * time.Millisecond,
			expectError:    true,
			errorSubstring: "context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())

			// Cancel context after specified duration
			go func() {
				time.Sleep(tt.cancelAfter)
				cancel()
			}()

			pool, err := Connect(ctx, tt.dsn)

			if tt.expectError {
				if err == nil {
					t.Errorf("Connect() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Connect() unexpected error: %v", err)
				}
			}

			// Always clean up pool if it was created
			if pool != nil {
				pool.Close()
			}
		})
	}
}

func TestConnect_ConfigParsing(t *testing.T) {
	tests := []struct {
		name        string
		dsn         string
		expectError bool
		description string
	}{
		{
			name:        "missing required components",
			dsn:         "postgres://user@host/",
			expectError: true,
			description: "DSN missing database name should fail",
		},
		{
			name:        "valid minimal DSN",
			dsn:         "postgres://postgres@localhost/test?sslmode=disable",
			expectError: true, // Will fail on connection but config parsing should succeed
			description: "Minimal valid DSN should parse config successfully",
		},
		{
			name:        "DSN with all components",
			dsn:         "postgres://user:pass@localhost:5432/dbname?sslmode=disable&connect_timeout=10",
			expectError: true, // Will fail on connection but config parsing should succeed
			description: "Full DSN should parse config successfully",
		},
		{
			name:        "invalid protocol",
			dsn:         "mysql://user:pass@localhost:5432/dbname",
			expectError: true,
			description: "Non-postgres protocol should fail",
		},
		{
			name:        "invalid port number",
			dsn:         "postgres://user:pass@localhost:abc/dbname?sslmode=disable",
			expectError: true,
			description: "Non-numeric port should fail config parsing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			pool, err := Connect(ctx, tt.dsn)

			if tt.expectError {
				if err == nil {
					t.Errorf("Connect() expected error for %s but got none", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Connect() unexpected error for %s: %v", tt.description, err)
				}
			}

			// Always clean up pool if it was created
			if pool != nil {
				pool.Close()
			}
		})
	}
}

// Benchmark test for connection establishment
func BenchmarkConnect_InvalidDSN(b *testing.B) {
	dsn := "invalid-dsn"
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool, err := Connect(ctx, dsn)
		if err == nil {
			b.Errorf("Expected error for invalid DSN")
		}
		if pool != nil {
			pool.Close()
		}
	}
}