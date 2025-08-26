package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nsqio/go-nsq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/austindbirch/harbor_hook/internal/config"
	"github.com/austindbirch/harbor_hook/internal/delivery"
	"github.com/austindbirch/harbor_hook/internal/metrics"
)

const (
	topic     = "deliveries"
	channel   = "workers"
	dlqTopic  = "deliveries_dlq"
	sigHeader = "X-HarborHook-Signature" // sha256=<hex>
	tsHeader  = "X-HarborHook-Timestamp" // unix seconds
)

type retryCfg struct {
	maxAttempts int
	backoff     []time.Duration
	jitterPct   float64
	publishDLQ  bool
}

// readRetryCfg reads the retry configuration from env vars.
func readRetryCfg() retryCfg {
	// Try to parse maxAttempts
	maxAttempts := parseEnvInt("MAX_ATTEMPTS", 6)
	// Try to parse backoff schedule
	js := os.Getenv("BACKOFF_SCHEDULE")
	if js == "" {
		js = "1s,4s,16s,1m,4m,10m"
	}
	var schedule []time.Duration
	for _, p := range strings.Split(js, ",") {
		d, err := time.ParseDuration(strings.TrimSpace(p))
		if err == nil {
			schedule = append(schedule, d)
		}
	}
	// If schedule is empty, use default values
	if len(schedule) == 0 {
		schedule = []time.Duration{
			time.Second,
			4 * time.Second,
			16 * time.Second,
			time.Minute,
			4 * time.Minute,
			10 * time.Minute,
		}
	}
	// Try to parse jitter percentage
	jitter := parseEnvFloat("BACKOFF_JITTER_PCT", 0.25)
	// Try to parse publish DLQ
	pubDLQ := strings.EqualFold(os.Getenv("PUBLISH_DLQ_TOPIC"), "true")
	return retryCfg{
		maxAttempts: maxAttempts,
		backoff:     schedule,
		jitterPct:   jitter,
		publishDLQ:  pubDLQ,
	}
}

// Helper func to parse int env vars, default to def
func parseEnvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

// Helper func to parse float env vars, default to def
func parseEnvFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func main() {
	cfg := config.FromEnv()
	ctx := context.Background()
	rand.NewSource(time.Now().UnixNano())

	// DB connect
	pool, err := pgxpool.New(ctx, cfg.DSN())
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	// Prom metrics
	reg := prometheus.NewRegistry()
	metrics.MustRegister(reg)

	// HTTP health/metrics
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	httpSrv := &http.Server{Addr: ":8082", Handler: mux}
	go func() {
		log.Printf("worker HTTP listening on %s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("worker http: %v", err)
		}
	}()

	// NSQ consumer
	conf := nsq.NewConfig()
	conf.MaxInFlight = 100
	consumer, err := nsq.NewConsumer(topic, channel, conf)
	if err != nil {
		log.Fatalf("nsq consumer: %v", err)
	}

	// DLQ producer
	var dlqProducer *nsq.Producer
	retry := readRetryCfg()
	if retry.publishDLQ {
		dlqProducer, err = nsq.NewProducer(cfg.NSQ.NsqdTCPAddr, nsq.NewConfig())
		if err != nil {
			log.Fatalf("nsq producer for DLQ: %v", err)
		}
		defer dlqProducer.Stop()
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}

	consumer.AddHandler(nsq.HandlerFunc(func(m *nsq.Message) error {
		var t delivery.Task
		if err := json.Unmarshal(m.Body, &t); err != nil {
			log.Printf("bad task: %v", err)
			metrics.DeliveriesTotal.WithLabelValues("failed").Inc()
			return nil
		}

		// Fetch endpoint secret for signing
		var secret string
		err := pool.QueryRow(ctx, `
			SELECT secret 
			FROM harborhook.endpoints 
			WHERE id=$1`,
			t.EndpointID).Scan(&secret)
		if err != nil || secret == "" {
			log.Printf("No secret for endpoint %s: %v", t.EndpointID, err)
			metrics.DeliveriesTotal.WithLabelValues("failed").Inc()
			return nil
		}

		// Build request (sign: HMAC over body||timestamp)
		body, _ := json.Marshal(t.Payload)
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		mac.Write([]byte(ts))
		sig := hex.EncodeToString(mac.Sum(nil))

		req, _ := http.NewRequest(http.MethodPost, t.EndpointURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(tsHeader, ts)
		req.Header.Set(sigHeader, "sha256="+sig)

		start := time.Now()
		resp, doErr := httpClient.Do(req)
		latency := time.Since(start)
		status := 0
		if doErr == nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
		}

		ok := (doErr == nil && status >= 200 && status < 300)
		if ok {
			// success: attempt = attempt+1, status=ok
			_, updErr := pool.Exec(ctx, `
				UPDATE harborhook.deliveries
				SET status='ok', attempt=attempt+1, http_status=$1, latency_ms=$2, updated_at=now(), last_error=NULL
				WHERE id=$3`,
				status, int(latency.Milliseconds()), t.DeliveryID,
			)
			if updErr != nil {
				log.Printf("db update success %s: %v", t.DeliveryID, updErr)
			}
			metrics.DeliveriesTotal.WithLabelValues("ok").Inc()
			return nil
		}

		// failure: increment attempt and decide requeue vs DLQ
		var newAttempt int
		_, updErr := pool.Exec(ctx, `
			UPDATE harborhook.deliveries
			SET status='failed', attempt=attempt+1, http_status=$1, latency_ms=$2, updated_at=now(), last_error=$3
			WHERE id=$4`,
			status, int(latency.Milliseconds()), errString(doErr), t.DeliveryID,
		)
		if updErr != nil {
			log.Printf("db update fail %s: %v", t.DeliveryID, updErr)
		}

		// fetch current attempt
		if err := pool.QueryRow(ctx, `SELECT attempt FROM harborhook.deliveries WHERE id=$1`, t.DeliveryID).Scan(&newAttempt); err != nil {
			log.Printf("read attempt %s: %v", t.DeliveryID, err)
			newAttempt = retry.maxAttempts // be safe -> DLQ
		}

		// classify reason for metrics
		reason := classifyReason(doErr, status)
		metrics.RetriesTotal.WithLabelValues(reason).Inc()

		if newAttempt >= retry.maxAttempts {
			// DLQ
			_, qErr := pool.Exec(ctx, `
				INSERT INTO harborhook.dlq(delivery_id, reason) VALUES ($1,$2)`,
				t.DeliveryID, fmt.Sprintf("max attempts reached (%d), last status=%d, err=%s", newAttempt, status, errString(doErr)),
			)
			if qErr != nil {
				log.Printf("dlq insert %s: %v", t.DeliveryID, qErr)
			}
			metrics.DLQTotal.Inc()

			if retry.publishDLQ && dlqProducer != nil {
				_ = dlqProducer.Publish(dlqTopic, m.Body) // best-effort
			}
			return nil // drop message; it's in DLQ now
		}

		// compute backoff with jitter and requeue
		delay := computeDelay(newAttempt, retry.backoff, retry.jitterPct)
		m.Requeue(delay)
		return nil
	}))

	// Connecting directly to NSQD forces channel creation, instead of the channel being lazily created on first publish
	if err := consumer.ConnectToNSQD(cfg.NSQ.NsqdTCPAddr); err != nil {
		log.Fatalf("connect to nsqd: %v", err)
	}
	if err := consumer.ConnectToNSQLookupd(cfg.NSQ.LookupHTTPAddr); err != nil {
		log.Fatalf("connect to lookupd: %v", err)
	}

	// Graceful stop
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	consumer.Stop()
	<-consumer.StopChan
	_ = httpSrv.Shutdown(context.Background())
	log.Println("worker stopped")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func computeDelay(attempt int, schedule []time.Duration, jitterPct float64) time.Duration {
	// attempt is 1-based after increment; map to schedule index
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(schedule) {
		idx = len(schedule) - 1
	}
	base := schedule[idx]
	// jitter: +/- jitterPct
	j := 1 + (rand.Float64()*2-1)*jitterPct
	if j < 0.1 {
		j = 0.1
	}
	return time.Duration(float64(base) * j)
}

func classifyReason(doErr error, status int) string {
	if doErr != nil {
		if strings.Contains(strings.ToLower(doErr.Error()), "timeout") {
			return "timeout"
		}
		return "network"
	}
	if status >= 500 {
		return "http_5xx"
	}
	if status == 429 {
		return "http_429"
	}
	return "other"
}
