package config

import (
	"os"
	"testing"
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
				"APP_NAME":              "test-app",
				"HTTP_PORT":             ":3000",
				"GRPC_PORT":             ":9000",
				"DB_USER":               "testuser",
				"DB_PASS":               "testpass",
				"DB_HOST":               "testhost",
				"DB_PORT":               "5433",
				"DB_NAME":               "testdb",
				"NSQD_TCP_ADDR":         "test-nsqd:4150",
				"NSQ_LOOKUP_HTTP_ADDR":  "http://test-nsqlookupd:4161",
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