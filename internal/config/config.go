package config

import (
	"fmt"
	"os"
)

type DB struct {
	User string
	Pass string
	Host string
	Port string
	Name string
}

type NSQ struct {
	NsqdTCPAddr    string // e.g. nsqd:4150
	LookupHTTPAddr string // e.g. http://nsqlookupd:4161
}

type Config struct {
	AppName  string
	HTTPPort string // :8080
	GRPCPort string // :50051
	DB       DB
	NSQ      NSQ
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
			NsqdTCPAddr:    getenv("NSQD_TCP_ADDR", "nsqd:4150"),
			LookupHTTPAddr: getenv("NSQ_LOOKUP_HTTP_ADDR", "http://nsqlookupd:4161"),
		},
	}
}

func (c Config) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.DB.User, c.DB.Pass, c.DB.Host, c.DB.Port, c.DB.Name)
}
