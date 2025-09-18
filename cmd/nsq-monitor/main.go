package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NSQStats represents the JSON structure returned by NSQ stats API
type NSQStats struct {
	Topics []struct {
		TopicName string `json:"topic_name"`
		Channels  []struct {
			ChannelName string `json:"channel_name"`
			Depth       int64  `json:"depth"`
			InFlightCount int64 `json:"in_flight_count"`
		} `json:"channels"`
		Depth int64 `json:"depth"`
	} `json:"topics"`
}

var (
	// Total queue backlog - what we really care about
	queueBacklog = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "harborhook_queue_backlog",
		Help: "Total number of messages waiting in the deliveries queue",
	})

	// Channel-specific metrics
	channelDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "harborhook_nsq_channel_depth",
		Help: "Depth of NSQ channels by topic and channel",
	}, []string{"topic", "channel"})

	channelInflight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "harborhook_nsq_channel_inflight",
		Help: "In-flight messages for NSQ channels by topic and channel",
	}, []string{"topic", "channel"})
)

func init() {
	prometheus.MustRegister(queueBacklog)
	prometheus.MustRegister(channelDepth)
	prometheus.MustRegister(channelInflight)
}

func main() {
	nsqdHost := getEnv("NSQD_HOST", "nsqd:4151")
	port := getEnv("PORT", "8084")
	interval := getEnvInt("POLL_INTERVAL_SECONDS", 15)

	log.Printf("NSQ Monitor starting on port %s", port)
	log.Printf("Monitoring NSQ at %s every %d seconds", nsqdHost, interval)

	// Start metrics collection in background
	go collectMetrics(nsqdHost, time.Duration(interval)*time.Second)

	// Expose metrics endpoint
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func collectMetrics(nsqdHost string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if err := updateMetrics(nsqdHost); err != nil {
			log.Printf("Error updating metrics: %v", err)
		}
	}
}

func updateMetrics(nsqdHost string) error {
	resp, err := http.Get(fmt.Sprintf("http://%s/stats?format=json", nsqdHost))
	if err != nil {
		return fmt.Errorf("failed to get NSQ stats: %w", err)
	}
	defer resp.Body.Close()

	var stats NSQStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return fmt.Errorf("failed to decode NSQ stats: %w", err)
	}

	// Update metrics
	for _, topic := range stats.Topics {
		if topic.TopicName == "deliveries" {
			for _, channel := range topic.Channels {
				if channel.ChannelName == "workers" {
					// This is the main queue backlog metric
					queueBacklog.Set(float64(channel.Depth))
				}
				// Update channel-specific metrics
				channelDepth.WithLabelValues(topic.TopicName, channel.ChannelName).Set(float64(channel.Depth))
				channelInflight.WithLabelValues(topic.TopicName, channel.ChannelName).Set(float64(channel.InFlightCount))
			}
		}
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}