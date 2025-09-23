package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type DB struct {
	User string
	Pass string
	Host string
	Port string
	Name string
}

type NSQ struct {
	NsqdTCPAddr     string // e.g. nsqd:4150
	LookupHTTPAddr  string // e.g. http://nsqlookupd:4161
	DeliveriesTopic string // NSQ topic for webhook deliveries
	DLQTopic        string // Dead letter queue topic
	WorkerChannel   string // NSQ channel name for workers
	SignatureHeader string // HTTP header for webhook signature
	TimestampHeader string // HTTP header for webhook timestamp
}

type Worker struct {
	MaxAttempts     int             // Maximum delivery attempts
	BackoffSchedule []time.Duration // Retry backoff durations
	JitterPercent   float64         // Backoff jitter percentage (0.0-1.0)
	PublishDLQ      bool            // Whether to publish failed deliveries to DLQ
	HTTPPort        string          // Worker HTTP metrics port
}

type FakeReceiver struct {
	FailFirstN           int           // Number of requests to fail initially
	EndpointSecret       string        // Secret for webhook signature verification
	SigningLeewaySeconds int           // Allowed timestamp skew in seconds
	ResponseDelayMS      int           // Simulated response delay in milliseconds
	Port                 string        // Server listen port
	ReadTimeout          time.Duration // HTTP read timeout
	WriteTimeout         time.Duration // HTTP write timeout
	IdleTimeout          time.Duration // HTTP idle timeout
}

type Config struct {
	AppName      string
	HTTPPort     string // :8080
	GRPCPort     string // :50051
	DB           DB
	NSQ          NSQ
	Worker       Worker
	FakeReceiver FakeReceiver
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func getenvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getenvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func parseBackoffSchedule(schedule string) []time.Duration {
	if schedule == "" {
		return []time.Duration{1 * time.Second, 4 * time.Second, 16 * time.Second, 1 * time.Minute, 4 * time.Minute, 10 * time.Minute}
	}

	parts := strings.Split(schedule, ",")
	durations := make([]time.Duration, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if d, err := time.ParseDuration(part); err == nil {
			durations = append(durations, d)
		}
	}

	if len(durations) == 0 {
		// Fallback to default if parsing failed
		return []time.Duration{1 * time.Second, 4 * time.Second, 16 * time.Second, 1 * time.Minute, 4 * time.Minute, 10 * time.Minute}
	}

	return durations
}

func FromEnv() Config {
	return Config{
		AppName:  getenv("APP_NAME", "harborhook"),
		HTTPPort: getenv("HTTP_PORT", ":8080"),
		GRPCPort: getenv("GRPC_PORT", ":50051"),
		DB: DB{
			User: getenv("DB_USER", "postgres"),
			Pass: getenv("DB_PASS", "postgres"),
			Host: getenv("DB_HOST", "postgres"),
			Port: getenv("DB_PORT", "5432"),
			Name: getenv("DB_NAME", "harborhook"),
		},
		NSQ: NSQ{
			NsqdTCPAddr:     getenv("NSQD_TCP_ADDR", "nsqd:4150"),
			LookupHTTPAddr:  getenv("NSQ_LOOKUP_HTTP_ADDR", "http://nsqlookupd:4161"),
			DeliveriesTopic: getenv("NSQ_DELIVERIES_TOPIC", "deliveries"),
			DLQTopic:        getenv("NSQ_DLQ_TOPIC", "deliveries_dlq"),
			WorkerChannel:   getenv("NSQ_WORKER_CHANNEL", "workers"),
			SignatureHeader: getenv("WEBHOOK_SIGNATURE_HEADER", "X-HarborHook-Signature"),
			TimestampHeader: getenv("WEBHOOK_TIMESTAMP_HEADER", "X-HarborHook-Timestamp"),
		},
		Worker: Worker{
			MaxAttempts:     getenvInt("MAX_ATTEMPTS", 6),
			BackoffSchedule: parseBackoffSchedule(getenv("BACKOFF_SCHEDULE", "")),
			JitterPercent:   getenvFloat("BACKOFF_JITTER_PCT", 0.25),
			PublishDLQ:      getenvBool("PUBLISH_DLQ_TOPIC", false),
			HTTPPort:        ":" + getenv("WORKER_HTTP_PORT", "8083"),
		},
		FakeReceiver: FakeReceiver{
			FailFirstN:           getenvInt("FAIL_FIRST_N", 0),
			EndpointSecret:       getenv("ENDPOINT_SECRET", ""),
			SigningLeewaySeconds: getenvInt("SIGNING_LEEWAY_SECONDS", 300),
			ResponseDelayMS:      getenvInt("RESPONSE_DELAY_MS", 0),
			Port:                 getenv("FAKE_RECEIVER_PORT", ":8081"),
			ReadTimeout:          getenvDuration("FAKE_RECEIVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:         getenvDuration("FAKE_RECEIVER_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:          getenvDuration("FAKE_RECEIVER_IDLE_TIMEOUT", 60*time.Second),
		},
	}
}

func (c Config) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.DB.User, c.DB.Pass, c.DB.Host, c.DB.Port, c.DB.Name)
}
