package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestUpdateMetrics(t *testing.T) {
	type label struct {
		topic   string
		channel string
	}

	testCases := []struct {
		name        string
		payload     string
		status      int
		wantErr     bool
		wantQueue   float64
		wantDepth   map[label]float64
		wantInflight map[label]float64
	}{
		{
			name: "deliveries workers channel updates metrics",
			payload: `{
				"topics": [
					{
						"topic_name": "deliveries",
						"channels": [
							{"channel_name": "workers", "depth": 10, "in_flight_count": 4},
							{"channel_name": "retries", "depth": 3, "in_flight_count": 1}
						],
						"depth": 13
					}
				]
			}`,
			wantQueue: 10,
			wantDepth: map[label]float64{
				{topic: "deliveries", channel: "workers"}: 10,
				{topic: "deliveries", channel: "retries"}: 3,
			},
			wantInflight: map[label]float64{
				{topic: "deliveries", channel: "workers"}: 4,
				{topic: "deliveries", channel: "retries"}: 1,
			},
		},
		{
			name: "deliveries without workers retains backlog",
			payload: `{
				"topics": [
					{
						"topic_name": "deliveries",
						"channels": [
							{"channel_name": "retries", "depth": 5, "in_flight_count": 2}
						],
						"depth": 5
					}
				]
			}`,
			wantQueue: 0,
			wantDepth: map[label]float64{
				{topic: "deliveries", channel: "retries"}: 5,
			},
			wantInflight: map[label]float64{
				{topic: "deliveries", channel: "retries"}: 2,
			},
		},
		{
			name:    "invalid payload returns error",
			payload: `invalid-json`,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			queueBacklog.Set(0)
			channelDepth.Reset()
			channelInflight.Reset()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/stats" {
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.payload))
			}))
			defer server.Close()

			host := strings.TrimPrefix(server.URL, "http://")
			err := updateMetrics(host)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("updateMetrics returned error: %v", err)
			}

			if got := testutil.ToFloat64(queueBacklog); got != tc.wantQueue {
				t.Fatalf("queueBacklog = %v, want %v", got, tc.wantQueue)
			}

			for lbl, want := range tc.wantDepth {
				got := testutil.ToFloat64(channelDepth.WithLabelValues(lbl.topic, lbl.channel))
				if got != want {
					t.Fatalf("channelDepth[%s/%s] = %v, want %v", lbl.topic, lbl.channel, got, want)
				}
			}

			for lbl, want := range tc.wantInflight {
				got := testutil.ToFloat64(channelInflight.WithLabelValues(lbl.topic, lbl.channel))
				if got != want {
					t.Fatalf("channelInflight[%s/%s] = %v, want %v", lbl.topic, lbl.channel, got, want)
				}
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	testCases := []struct {
		name        string
		key         string
		value       string
		set         bool
		defaultVal  string
		want        string
	}{
		{
			name:       "returns existing value",
			key:        "NSQ_MONITOR_TEST_ENV_PRESENT",
			value:      "custom",
			set:        true,
			defaultVal: "default",
			want:       "custom",
		},
		{
			name:       "returns default when unset",
			key:        "NSQ_MONITOR_TEST_ENV_UNSET",
			defaultVal: "default",
			want:       "default",
		},
		{
			name:       "returns default when empty string",
			key:        "NSQ_MONITOR_TEST_ENV_EMPTY",
			value:      "",
			set:        true,
			defaultVal: "fallback",
			want:       "fallback",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(tc.key, tc.value)
			}
			if got := getEnv(tc.key, tc.defaultVal); got != tc.want {
				t.Fatalf("getEnv(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	testCases := []struct {
		name       string
		key        string
		value      string
		set        bool
		defaultVal int
		want       int
	}{
		{
			name:       "parses valid integer",
			key:        "NSQ_MONITOR_TEST_INT_VALID",
			value:      "42",
			set:        true,
			defaultVal: 15,
			want:       42,
		},
		{
			name:       "returns default on invalid integer",
			key:        "NSQ_MONITOR_TEST_INT_INVALID",
			value:      "not-an-int",
			set:        true,
			defaultVal: 15,
			want:       15,
		},
		{
			name:       "returns default when unset",
			key:        "NSQ_MONITOR_TEST_INT_UNSET",
			defaultVal: 10,
			want:       10,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(tc.key, tc.value)
			}
			if got := getEnvInt(tc.key, tc.defaultVal); got != tc.want {
				t.Fatalf("getEnvInt(%q) = %d, want %d", tc.key, got, tc.want)
			}
		})
	}
}
