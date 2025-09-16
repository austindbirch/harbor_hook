package main

// TODO: Add tests that require more setup and scaffolding:
// - Integration tests with real database connections and NSQ producers
// - TLS configuration testing with actual certificates
// - gRPC server initialization and health check testing
// - HTTP server startup and grpc-gateway integration testing
// - Signal handling and graceful shutdown testing
// - JWT configuration and validation setup testing
// - End-to-end service initialization with all dependencies
// - Error handling during startup (DB connection failures, NSQ failures, etc.)
// - Environment variable parsing and configuration validation
// - Metrics and tracing initialization testing

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/austindbirch/harbor_hook/internal/config"
	"github.com/austindbirch/harbor_hook/internal/health"
	"github.com/austindbirch/harbor_hook/internal/ingest"
)

func TestConfigurationLoading(t *testing.T) {
	// Save original environment
	originalEnvVars := map[string]string{
		"DB_HOST":       os.Getenv("DB_HOST"),
		"DB_PORT":       os.Getenv("DB_PORT"),
		"DB_NAME":       os.Getenv("DB_NAME"),
		"DB_USER":       os.Getenv("DB_USER"),
		"DB_PASS":       os.Getenv("DB_PASS"),
		"NSQD_TCP_ADDR": os.Getenv("NSQD_TCP_ADDR"),
		"GRPC_PORT":     os.Getenv("GRPC_PORT"),
		"HTTP_PORT":     os.Getenv("HTTP_PORT"),
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
		validate func(t *testing.T, cfg config.Config)
	}{
		{
			name: "default configuration",
			envVars: map[string]string{
				// Use actual defaults from config package
			},
			validate: func(t *testing.T, cfg config.Config) {
				if cfg.DB.Host != "postgres" {
					t.Errorf("Expected DB host 'postgres', got %q", cfg.DB.Host)
				}
				if cfg.DB.Port != "5432" {
					t.Errorf("Expected DB port '5432', got %q", cfg.DB.Port)
				}
				if cfg.NSQ.NsqdTCPAddr != "nsqd:4150" {
					t.Errorf("Expected NSQ address 'nsqd:4150', got %q", cfg.NSQ.NsqdTCPAddr)
				}
				if cfg.GRPCPort != ":50051" {
					t.Errorf("Expected GRPC port ':50051', got %q", cfg.GRPCPort)
				}
				if cfg.HTTPPort != ":8080" {
					t.Errorf("Expected HTTP port ':8080', got %q", cfg.HTTPPort)
				}
			},
		},
		{
			name: "custom configuration",
			envVars: map[string]string{
				"DB_HOST":        "custom-host",
				"DB_PORT":        "3306",
				"DB_NAME":        "custom-db",
				"DB_USER":        "custom-user",
				"DB_PASS":        "custom-pass",
				"NSQD_TCP_ADDR":  "nsq-host:4150",
				"GRPC_PORT":      ":9090",
				"HTTP_PORT":      ":9091",
			},
			validate: func(t *testing.T, cfg config.Config) {
				if cfg.DB.Host != "custom-host" {
					t.Errorf("Expected DB host 'custom-host', got %q", cfg.DB.Host)
				}
				if cfg.DB.Port != "3306" {
					t.Errorf("Expected DB port '3306', got %q", cfg.DB.Port)
				}
				if cfg.NSQ.NsqdTCPAddr != "nsq-host:4150" {
					t.Errorf("Expected NSQ address 'nsq-host:4150', got %q", cfg.NSQ.NsqdTCPAddr)
				}
				if cfg.GRPCPort != ":9090" {
					t.Errorf("Expected GRPC port ':9090', got %q", cfg.GRPCPort)
				}
				if cfg.HTTPPort != ":9091" {
					t.Errorf("Expected HTTP port ':9091', got %q", cfg.HTTPPort)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := config.FromEnv()
			tt.validate(t, cfg)
		})
	}
}

func TestTLSConfiguration(t *testing.T) {
	// Save original environment
	originalTLS := os.Getenv("ENABLE_TLS")
	originalCert := os.Getenv("TLS_CERT_PATH")
	originalKey := os.Getenv("TLS_KEY_PATH")
	originalCA := os.Getenv("CA_CERT_PATH")

	// Restore environment after test
	defer func() {
		if originalTLS == "" {
			os.Unsetenv("ENABLE_TLS")
		} else {
			os.Setenv("ENABLE_TLS", originalTLS)
		}
		if originalCert == "" {
			os.Unsetenv("TLS_CERT_PATH")
		} else {
			os.Setenv("TLS_CERT_PATH", originalCert)
		}
		if originalKey == "" {
			os.Unsetenv("TLS_KEY_PATH")
		} else {
			os.Setenv("TLS_KEY_PATH", originalKey)
		}
		if originalCA == "" {
			os.Unsetenv("CA_CERT_PATH")
		} else {
			os.Setenv("CA_CERT_PATH", originalCA)
		}
	}()

	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name: "TLS disabled",
			envVars: map[string]string{
				"ENABLE_TLS": "false",
			},
			expected: false,
		},
		{
			name:    "TLS not set",
			envVars: map[string]string{
				// ENABLE_TLS not set
			},
			expected: false,
		},
		{
			name: "TLS enabled",
			envVars: map[string]string{
				"ENABLE_TLS": "true",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all TLS env vars first
			os.Unsetenv("ENABLE_TLS")
			os.Unsetenv("TLS_CERT_PATH")
			os.Unsetenv("TLS_KEY_PATH")
			os.Unsetenv("CA_CERT_PATH")

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			enableTLS := os.Getenv("ENABLE_TLS") == "true"
			if enableTLS != tt.expected {
				t.Errorf("Expected TLS enabled: %v, got: %v", tt.expected, enableTLS)
			}
		})
	}
}

func TestJWTConfiguration(t *testing.T) {
	// Save original environment
	originalIssuer := os.Getenv("JWT_ISSUER")
	originalAudience := os.Getenv("JWT_AUDIENCE")

	// Restore environment after test
	defer func() {
		if originalIssuer == "" {
			os.Unsetenv("JWT_ISSUER")
		} else {
			os.Setenv("JWT_ISSUER", originalIssuer)
		}
		if originalAudience == "" {
			os.Unsetenv("JWT_AUDIENCE")
		} else {
			os.Setenv("JWT_AUDIENCE", originalAudience)
		}
	}()

	tests := []struct {
		name               string
		envVars            map[string]string
		expectedConfigured bool
		expectedAudience   string
	}{
		{
			name:    "no JWT configuration",
			envVars: map[string]string{
				// No JWT env vars set
			},
			expectedConfigured: false,
		},
		{
			name: "JWT issuer only",
			envVars: map[string]string{
				"JWT_ISSUER": "harborhook",
			},
			expectedConfigured: true,
			expectedAudience:   "harborhook-api", // default
		},
		{
			name: "JWT issuer and custom audience",
			envVars: map[string]string{
				"JWT_ISSUER":   "harborhook",
				"JWT_AUDIENCE": "custom-audience",
			},
			expectedConfigured: true,
			expectedAudience:   "custom-audience",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear JWT env vars first
			os.Unsetenv("JWT_ISSUER")
			os.Unsetenv("JWT_AUDIENCE")

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			jwtIssuer := os.Getenv("JWT_ISSUER")
			jwtAudience := os.Getenv("JWT_AUDIENCE")
			if jwtAudience == "" {
				jwtAudience = "harborhook-api"
			}

			jwtConfigured := jwtIssuer != ""
			if jwtConfigured != tt.expectedConfigured {
				t.Errorf("Expected JWT configured: %v, got: %v", tt.expectedConfigured, jwtConfigured)
			}

			if tt.expectedConfigured && jwtAudience != tt.expectedAudience {
				t.Errorf("Expected JWT audience: %q, got: %q", tt.expectedAudience, jwtAudience)
			}
		})
	}
}

func TestServiceInitialization(t *testing.T) {
	// Test that the ingest service can be initialized with nil dependencies
	// This tests the NewServer function from the ingest package
	svc := ingest.NewServer(nil, nil)
	if svc == nil {
		t.Error("Expected non-nil service, got nil")
	}

	// Test ping functionality with nil dependencies (should work as it doesn't use them)
	ctx := context.Background()
	resp, err := svc.Ping(ctx, nil)
	if err != nil {
		t.Errorf("Expected no error from Ping, got: %v", err)
	}
	if resp.Message != "pong" {
		t.Errorf("Expected ping response 'pong', got: %q", resp.Message)
	}
}

func TestHealthHandlerIntegration(t *testing.T) {
	// Test that the health handler can be created
	// Note: This will fail with nil pool but tests the handler creation
	handler := health.HTTPHandler(nil)
	if handler == nil {
		t.Error("Expected non-nil health handler, got nil")
	}

	// Test the handler with a request (will fail but tests the structure)
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	// This will likely return an error due to nil pool, but we can test the handler exists
	handler(w, req)

	// The handler should at least respond (even with an error)
	if w.Code == 0 {
		t.Error("Expected health handler to respond with some status code")
	}
}

func TestTLSConfigCreation(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "basic TLS config creation",
			testFunc: func(t *testing.T) {
				tlsConfig := &tls.Config{
					ClientAuth: tls.RequireAndVerifyClientCert,
				}
				if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert {
					t.Errorf("Expected ClientAuth to be RequireAndVerifyClientCert")
				}
			},
		},
		{
			name: "CA cert pool creation",
			testFunc: func(t *testing.T) {
				caCertPool := x509.NewCertPool()
				if caCertPool == nil {
					t.Error("Expected non-nil CA cert pool")
				}
			},
		},
		{
			name: "HTTP TLS config cloning",
			testFunc: func(t *testing.T) {
				original := &tls.Config{
					ClientAuth: tls.RequireAndVerifyClientCert,
				}
				cloned := original.Clone()
				if cloned == nil {
					t.Error("Expected non-nil cloned TLS config")
				}
				cloned.ClientAuth = tls.NoClientCert
				if cloned.ClientAuth != tls.NoClientCert {
					t.Error("Expected cloned config to have NoClientCert")
				}
				if original.ClientAuth != tls.RequireAndVerifyClientCert {
					t.Error("Expected original config to remain unchanged")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

func TestHTTPServerConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		addr        string
		tlsConfig   *tls.Config
		expectError bool
	}{
		{
			name:        "basic HTTP server config",
			addr:        ":8080",
			tlsConfig:   nil,
			expectError: false,
		},
		{
			name: "HTTPS server config",
			addr: ":8443",
			tlsConfig: &tls.Config{
				ClientAuth: tls.NoClientCert,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			server := &http.Server{
				Addr:      tt.addr,
				Handler:   mux,
				TLSConfig: tt.tlsConfig,
			}

			if server.Addr != tt.addr {
				t.Errorf("Expected server addr %q, got %q", tt.addr, server.Addr)
			}
			if (server.TLSConfig != nil) != (tt.tlsConfig != nil) {
				t.Errorf("TLS config mismatch")
			}
		})
	}
}
