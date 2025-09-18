package config

import (
	"os"
	"testing"
	"time"
)

func TestGetenv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		expected     string
	}{
		{
			name:         "returns environment variable when set",
			key:          "TEST_KEY_1",
			defaultValue: "default",
			envValue:     "env_value",
			expected:     "env_value",
		},
		{
			name:         "returns default when environment variable is empty",
			key:          "TEST_KEY_2",
			defaultValue: "default",
			envValue:     "",
			expected:     "default",
		},
		{
			name:         "returns default when environment variable is not set",
			key:          "TEST_KEY_3",
			defaultValue: "default",
			envValue:     "",
			expected:     "default",
		},
		{
			name:         "handles empty default value",
			key:          "TEST_KEY_4",
			defaultValue: "",
			envValue:     "env_value",
			expected:     "env_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			result := getenv(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getenv(%q, %q) = %q, want %q", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected Config
	}{
		{
			name:    "default values when no env vars set",
			envVars: map[string]string{},
			expected: Config{
				AppName:  "harborhook",
				HTTPPort: ":8080",
				GRPCPort: ":50051",
				DB: DB{
					User: "postgres",
					Pass: "postgres",
					Host: "postgres",
					Port: "5432",
					Name: "harborhook",
				},
				NSQ: NSQ{
					NsqdTCPAddr:    "nsqd:4150",
					LookupHTTPAddr: "http://nsqlookupd:4161",
				},
			},
		},
		{
			name: "custom values from environment",
			envVars: map[string]string{
				"APP_NAME":             "test-app",
				"HTTP_PORT":            ":3000",
				"GRPC_PORT":            ":9000",
				"DB_USER":              "testuser",
				"DB_PASS":              "testpass",
				"DB_HOST":              "testhost",
				"DB_PORT":              "5433",
				"DB_NAME":              "testdb",
				"NSQD_TCP_ADDR":        "test-nsqd:4150",
				"NSQ_LOOKUP_HTTP_ADDR": "http://test-nsqlookupd:4161",
			},
			expected: Config{
				AppName:  "test-app",
				HTTPPort: ":3000",
				GRPCPort: ":9000",
				DB: DB{
					User: "testuser",
					Pass: "testpass",
					Host: "testhost",
					Port: "5433",
					Name: "testdb",
				},
				NSQ: NSQ{
					NsqdTCPAddr:    "test-nsqd:4150",
					LookupHTTPAddr: "http://test-nsqlookupd:4161",
				},
			},
		},
		{
			name: "partial environment variables",
			envVars: map[string]string{
				"APP_NAME": "partial-app",
				"DB_HOST":  "custom-host",
				"DB_PORT":  "9999",
			},
			expected: Config{
				AppName:  "partial-app",
				HTTPPort: ":8080",
				GRPCPort: ":50051",
				DB: DB{
					User: "postgres",
					Pass: "postgres",
					Host: "custom-host",
					Port: "9999",
					Name: "harborhook",
				},
				NSQ: NSQ{
					NsqdTCPAddr:    "nsqd:4150",
					LookupHTTPAddr: "http://nsqlookupd:4161",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}
			defer func() {
				for key := range tt.envVars {
					os.Unsetenv(key)
				}
			}()

			result := FromEnv()

			if result.AppName != tt.expected.AppName {
				t.Errorf("AppName = %q, want %q", result.AppName, tt.expected.AppName)
			}
			if result.HTTPPort != tt.expected.HTTPPort {
				t.Errorf("HTTPPort = %q, want %q", result.HTTPPort, tt.expected.HTTPPort)
			}
			if result.GRPCPort != tt.expected.GRPCPort {
				t.Errorf("GRPCPort = %q, want %q", result.GRPCPort, tt.expected.GRPCPort)
			}

			if result.DB.User != tt.expected.DB.User {
				t.Errorf("DB.User = %q, want %q", result.DB.User, tt.expected.DB.User)
			}
			if result.DB.Pass != tt.expected.DB.Pass {
				t.Errorf("DB.Pass = %q, want %q", result.DB.Pass, tt.expected.DB.Pass)
			}
			if result.DB.Host != tt.expected.DB.Host {
				t.Errorf("DB.Host = %q, want %q", result.DB.Host, tt.expected.DB.Host)
			}
			if result.DB.Port != tt.expected.DB.Port {
				t.Errorf("DB.Port = %q, want %q", result.DB.Port, tt.expected.DB.Port)
			}
			if result.DB.Name != tt.expected.DB.Name {
				t.Errorf("DB.Name = %q, want %q", result.DB.Name, tt.expected.DB.Name)
			}

			if result.NSQ.NsqdTCPAddr != tt.expected.NSQ.NsqdTCPAddr {
				t.Errorf("NSQ.NsqdTCPAddr = %q, want %q", result.NSQ.NsqdTCPAddr, tt.expected.NSQ.NsqdTCPAddr)
			}
			if result.NSQ.LookupHTTPAddr != tt.expected.NSQ.LookupHTTPAddr {
				t.Errorf("NSQ.LookupHTTPAddr = %q, want %q", result.NSQ.LookupHTTPAddr, tt.expected.NSQ.LookupHTTPAddr)
			}
		})
	}
}

func TestConfig_DSN(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{
			name: "default postgres configuration",
			config: Config{
				DB: DB{
					User: "postgres",
					Pass: "postgres",
					Host: "localhost",
					Port: "5432",
					Name: "harborhook",
				},
			},
			want: "postgres://postgres:postgres@localhost:5432/harborhook?sslmode=disable",
		},
		{
			name: "custom database configuration",
			config: Config{
				DB: DB{
					User: "testuser",
					Pass: "testpass",
					Host: "db.example.com",
					Port: "5433",
					Name: "testdb",
				},
			},
			want: "postgres://testuser:testpass@db.example.com:5433/testdb?sslmode=disable",
		},
		{
			name: "configuration with special characters in password",
			config: Config{
				DB: DB{
					User: "user",
					Pass: "p@ssw0rd!",
					Host: "localhost",
					Port: "5432",
					Name: "mydb",
				},
			},
			want: "postgres://user:p@ssw0rd!@localhost:5432/mydb?sslmode=disable",
		},
		{
			name: "empty password",
			config: Config{
				DB: DB{
					User: "user",
					Pass: "",
					Host: "localhost",
					Port: "5432",
					Name: "mydb",
				},
			},
			want: "postgres://user:@localhost:5432/mydb?sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.DSN()
			if got != tt.want {
				t.Errorf("Config.DSN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetenvInt(t *testing.T) {
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

			result := getenvInt(tt.envVar, tt.def)
			if result != tt.expected {
				t.Errorf("getenvInt(%q, %d) = %d, want %d", tt.envVar, tt.def, result, tt.expected)
			}
		})
	}
}

func TestGetenvFloat(t *testing.T) {
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

			result := getenvFloat(tt.envVar, tt.def)
			if result != tt.expected {
				t.Errorf("getenvFloat(%q, %f) = %f, want %f", tt.envVar, tt.def, result, tt.expected)
			}
		})
	}
}

func TestGetenvBool(t *testing.T) {
	// Save original environment
	originalValue := os.Getenv("TEST_BOOL_VAR")
	defer func() {
		if originalValue == "" {
			os.Unsetenv("TEST_BOOL_VAR")
		} else {
			os.Setenv("TEST_BOOL_VAR", originalValue)
		}
	}()

	tests := []struct {
		name     string
		envVar   string
		envValue string
		def      bool
		expected bool
	}{
		{
			name:     "true value",
			envVar:   "TEST_BOOL_VAR",
			envValue: "true",
			def:      false,
			expected: true,
		},
		{
			name:     "false value",
			envVar:   "TEST_BOOL_VAR",
			envValue: "false",
			def:      true,
			expected: false,
		},
		{
			name:     "True value (case insensitive)",
			envVar:   "TEST_BOOL_VAR",
			envValue: "True",
			def:      false,
			expected: true,
		},
		{
			name:     "1 value",
			envVar:   "TEST_BOOL_VAR",
			envValue: "1",
			def:      false,
			expected: true,
		},
		{
			name:     "0 value",
			envVar:   "TEST_BOOL_VAR",
			envValue: "0",
			def:      true,
			expected: false,
		},
		{
			name:     "invalid value uses default",
			envVar:   "TEST_BOOL_VAR",
			envValue: "not-a-bool",
			def:      true,
			expected: true,
		},
		{
			name:     "empty string uses default",
			envVar:   "TEST_BOOL_VAR",
			envValue: "",
			def:      true,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv(tt.envVar)
			} else {
				os.Setenv(tt.envVar, tt.envValue)
			}

			result := getenvBool(tt.envVar, tt.def)
			if result != tt.expected {
				t.Errorf("getenvBool(%q, %v) = %v, want %v", tt.envVar, tt.def, result, tt.expected)
			}
		})
	}
}

func TestGetenvDuration(t *testing.T) {
	// Save original environment
	originalValue := os.Getenv("TEST_DURATION_VAR")
	defer func() {
		if originalValue == "" {
			os.Unsetenv("TEST_DURATION_VAR")
		} else {
			os.Setenv("TEST_DURATION_VAR", originalValue)
		}
	}()

	tests := []struct {
		name     string
		envVar   string
		envValue string
		def      time.Duration
		expected time.Duration
	}{
		{
			name:     "valid duration seconds",
			envVar:   "TEST_DURATION_VAR",
			envValue: "30s",
			def:      10 * time.Second,
			expected: 30 * time.Second,
		},
		{
			name:     "valid duration minutes",
			envVar:   "TEST_DURATION_VAR",
			envValue: "5m",
			def:      10 * time.Second,
			expected: 5 * time.Minute,
		},
		{
			name:     "valid duration hours",
			envVar:   "TEST_DURATION_VAR",
			envValue: "2h",
			def:      10 * time.Second,
			expected: 2 * time.Hour,
		},
		{
			name:     "invalid duration uses default",
			envVar:   "TEST_DURATION_VAR",
			envValue: "not-a-duration",
			def:      10 * time.Second,
			expected: 10 * time.Second,
		},
		{
			name:     "empty string uses default",
			envVar:   "TEST_DURATION_VAR",
			envValue: "",
			def:      10 * time.Second,
			expected: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv(tt.envVar)
			} else {
				os.Setenv(tt.envVar, tt.envValue)
			}

			result := getenvDuration(tt.envVar, tt.def)
			if result != tt.expected {
				t.Errorf("getenvDuration(%q, %v) = %v, want %v", tt.envVar, tt.def, result, tt.expected)
			}
		})
	}
}

func TestParseBackoffSchedule(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		expected []time.Duration
	}{
		{
			name:     "empty string returns default",
			schedule: "",
			expected: []time.Duration{1 * time.Second, 4 * time.Second, 16 * time.Second, 1 * time.Minute, 4 * time.Minute, 10 * time.Minute},
		},
		{
			name:     "valid schedule",
			schedule: "1s,5s,30s",
			expected: []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second},
		},
		{
			name:     "schedule with spaces",
			schedule: "2s, 10s, 1m",
			expected: []time.Duration{2 * time.Second, 10 * time.Second, 1 * time.Minute},
		},
		{
			name:     "mixed valid and invalid returns valid only",
			schedule: "1s,invalid,5s",
			expected: []time.Duration{1 * time.Second, 5 * time.Second},
		},
		{
			name:     "all invalid returns default",
			schedule: "invalid,also-invalid",
			expected: []time.Duration{1 * time.Second, 4 * time.Second, 16 * time.Second, 1 * time.Minute, 4 * time.Minute, 10 * time.Minute},
		},
		{
			name:     "single value",
			schedule: "10s",
			expected: []time.Duration{10 * time.Second},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBackoffSchedule(tt.schedule)
			if len(result) != len(tt.expected) {
				t.Errorf("parseBackoffSchedule(%q) returned %d durations, want %d", tt.schedule, len(result), len(tt.expected))
				return
			}
			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("parseBackoffSchedule(%q)[%d] = %v, want %v", tt.schedule, i, result[i], expected)
				}
			}
		})
	}
}
