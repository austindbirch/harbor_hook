package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	"github.com/austindbirch/harbor_hook/internal/logging"
	"github.com/austindbirch/harbor_hook/internal/metrics"
	"github.com/austindbirch/harbor_hook/internal/tracing"

	"go.opentelemetry.io/otel/attribute"
)

func main() {
	cfg := config.FromEnv()
	ctx := context.Background()
	rand.NewSource(time.Now().UnixNano())

	// Initialize structured logging
	logger := logging.New("harborhook-worker")

	// Debug: Log the NSQ configuration
	logger.Plain().WithFields(map[string]any{
		"nsqd_tcp_addr":    cfg.NSQ.NsqdTCPAddr,
		"lookup_http_addr": cfg.NSQ.LookupHTTPAddr,
		"deliveries_topic": cfg.NSQ.DeliveriesTopic,
		"worker_channel":   cfg.NSQ.WorkerChannel,
	}).Info("NSQ configuration loaded")

	// Initialize OpenTelemetry tracing
	shutdown, err := tracing.InitTracing(ctx, "harborhook-worker")
	if err != nil {
		logger.Plain().WithError(err).Fatal("Failed to initialize tracing")
	}
	defer shutdown()

	// DB connect
	pool, err := pgxpool.New(ctx, cfg.DSN())
	if err != nil {
		logger.Plain().WithError(err).Fatal("db connect failed")
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
	httpPort := cfg.Worker.HTTPPort
	httpSrv := &http.Server{Addr: httpPort, Handler: mux}
	go func() {
		logger.Plain().WithField("addr", httpSrv.Addr).Info("worker HTTP server starting")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Plain().WithError(err).Fatal("worker HTTP server failed")
		}
	}()

	// NSQ consumer
	conf := nsq.NewConfig()
	conf.MaxInFlight = 1500
	consumer, err := nsq.NewConsumer(cfg.NSQ.DeliveriesTopic, cfg.NSQ.WorkerChannel, conf)
	if err != nil {
		logger.Plain().WithError(err).Fatal("nsq consumer creation failed")
	}

	// DLQ producer
	var dlqProducer *nsq.Producer
	if cfg.Worker.PublishDLQ {
		dlqProducer, err = nsq.NewProducer(cfg.NSQ.NsqdTCPAddr, nsq.NewConfig())
		if err != nil {
			logger.Plain().WithError(err).Fatal("nsq producer for DLQ creation failed")
		}
		defer dlqProducer.Stop()
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}

	// Start backlog monitoring
	startBacklogMonitor(cfg)

	consumer.AddHandler(nsq.HandlerFunc(func(m *nsq.Message) error {
		m.DisableAutoResponse() // we manually requeue or finish
		defer func() {
			if !m.HasResponded() {
				logger.Plain().Warn("message had no response, finishing")
				m.Finish()
			}
		}()

		var t delivery.Task
		if err := json.Unmarshal(m.Body, &t); err != nil {
			logger.Plain().WithError(err).Error("bad task payload")
			metrics.RecordDelivery("failed", "unknown", "unknown", 0)
			m.Finish() // terminal: don't retry bad payloads
			return nil
		}

		// Extract trace context from NSQ message headers and start span
		ctx := tracing.ExtractTraceFromNSQ(ctx, t.TraceHeaders)
		ctx, span := tracing.StartSpan(ctx, "worker.delivery",
			attribute.String("delivery_id", t.DeliveryID),
			attribute.String("event_id", t.EventID),
			attribute.String("tenant_id", t.TenantID),
			attribute.String("endpoint_id", t.EndpointID),
			attribute.String("endpoint_url", t.EndpointURL),
			attribute.String("event_type", t.EventType),
			attribute.Int("attempt", t.Attempt),
		)
		defer span.End()

		// Mark dequeued/inflight
		tracing.AddSpanEvent(ctx, "db.update_delivery_inflight")
		_, _ = pool.Exec(ctx, `
			UPDATE harborhook.deliveries
			SET status='inflight', dequeued_at=now(), updated_at=now()
			WHERE id=$1`, t.DeliveryID)

		// Fetch endpoint secret for signing
		tracing.AddSpanEvent(ctx, "db.fetch_endpoint_secret")
		var secret sql.NullString
		if err := pool.QueryRow(ctx, `SELECT secret FROM harborhook.endpoints WHERE id=$1`,
			t.EndpointID).Scan(&secret); err != nil || !secret.Valid || secret.String == "" {
			tracing.SetSpanError(ctx, err)
			_, _ = pool.Exec(ctx, `
				UPDATE harborhook.deliveries 
				SET status='failed', attempt=attempt+1, failed_at=now(), updated_at=now(), last_error='endpoint_secret_missing' 
				WHERE id=$1`, t.DeliveryID)
			logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithEndpoint(t.EndpointID).WithError(err).Error("No secret for endpoint")
			metrics.RecordDelivery("failed", t.TenantID, t.EndpointID, 0)
			m.Finish() // terminal: can't sign without secret
			return nil
		}

		// Build request (sign: HMAC over body||timestamp)
		tracing.AddSpanEvent(ctx, "http.sign_request")
		body, _ := json.Marshal(t.Payload)
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secret.String))
		mac.Write(body)
		mac.Write([]byte(ts))
		sig := hex.EncodeToString(mac.Sum(nil))

		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, t.EndpointURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(cfg.NSQ.TimestampHeader, ts)
		req.Header.Set(cfg.NSQ.SignatureHeader, "sha256="+sig)

		// Add trace ID to HTTP headers for correlation
		if traceID := tracing.GetTraceID(ctx); traceID != "" {
			req.Header.Set("X-Trace-Id", traceID)
		}

		start := time.Now()
		// record sent_at
		tracing.AddSpanEvent(ctx, "db.update_delivery_sent")
		_, _ = pool.Exec(ctx, `
			UPDATE harborhook.deliveries
			SET sent_at=$2, updated_at=now()
			WHERE id=$1`, t.DeliveryID, start)

		tracing.AddSpanEvent(ctx, "http.send_webhook")
		resp, doErr := httpClient.Do(req)
		latency := time.Since(start)
		status := 0
		if doErr == nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
		}

		// Add HTTP response attributes to span
		span.SetAttributes(
			attribute.Int("http.status_code", status),
			attribute.Int64("http.latency_ms", latency.Milliseconds()),
		)
		if doErr != nil {
			span.SetAttributes(attribute.String("http.error", doErr.Error()))
		}

		ok := (doErr == nil && status >= 200 && status < 300)
		if ok {
			// success: attempt+=, status=ok
			tracing.AddSpanEvent(ctx, "delivery.success")
			_, updErr := pool.Exec(ctx, `
				UPDATE harborhook.deliveries
				SET status='delivered', delivered_at=now(), attempt=attempt+1, http_status=$1, latency_ms=$2, updated_at=now(), last_error=NULL
				WHERE id=$3`,
				status, int(latency.Milliseconds()), t.DeliveryID,
			)
			if updErr != nil {
				logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithError(updErr).Error("db update success failed")
				tracing.SetSpanError(ctx, updErr)
			}
			// Record successful delivery with enhanced metrics
			metrics.RecordDelivery("delivered", t.TenantID, t.EndpointID, latency)
			metrics.RecordHTTPDelivery(t.TenantID, t.EndpointID, strconv.Itoa(status), latency)
			m.Finish() // explicit ack
			return nil
		}

		// failure: increment attempt and decide requeue vs DLQ
		tracing.AddSpanEvent(ctx, "delivery.failed")
		var newAttempt int
		_, updErr := pool.Exec(ctx, `
			UPDATE harborhook.deliveries
			SET status='failed', failed_at=now(), attempt=attempt+1, http_status=$1, latency_ms=$2, updated_at=now(), last_error=$3
			WHERE id=$4`,
			status, int(latency.Milliseconds()), errString(doErr), t.DeliveryID,
		)
		if updErr != nil {
			logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithError(updErr).Error("db update fail failed")
			tracing.SetSpanError(ctx, updErr)
		}

		// fetch current attempt
		if err := pool.QueryRow(ctx, `SELECT attempt FROM harborhook.deliveries WHERE id=$1`, t.DeliveryID).Scan(&newAttempt); err != nil {
			logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithError(err).Error("read attempt failed")
			tracing.SetSpanError(ctx, err)
			newAttempt = cfg.Worker.MaxAttempts // be safe -> DLQ
		}

		// classify reason for metrics and record enhanced metrics
		reason := classifyReason(doErr, status)
		span.SetAttributes(attribute.String("failure_reason", reason))
		metrics.RecordRetry(reason)
		metrics.RecordDelivery("failed", t.TenantID, t.EndpointID, latency)
		if status > 0 {
			metrics.RecordHTTPDelivery(t.TenantID, t.EndpointID, strconv.Itoa(status), latency)
		}

		if newAttempt >= cfg.Worker.MaxAttempts {
			// DLQ - Insert into DLQ table first
			tracing.AddSpanEvent(ctx, "delivery.dlq", attribute.Int("attempt", newAttempt))
			_, qErr := pool.Exec(ctx, `
				INSERT INTO harborhook.dlq(delivery_id, reason) VALUES ($1,$2)`,
				t.DeliveryID, fmt.Sprintf("max attempts reached (%d), last status=%d, err=%s", newAttempt, status, errString(doErr)),
			)
			if qErr != nil {
				logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithError(qErr).Error("dlq insert failed")
				tracing.SetSpanError(ctx, qErr)
			}

			// Update delivery status to dead (this will trigger our automatic dlq_at timestamp)
			_, updateErr := pool.Exec(ctx, `
				UPDATE harborhook.deliveries SET status='dead' WHERE id=$1`,
				t.DeliveryID,
			)
			if updateErr != nil {
				logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithError(updateErr).Error("dlq status update failed")
				tracing.SetSpanError(ctx, updateErr)
			}

			// DLQ (topic publish)
			if cfg.Worker.PublishDLQ && dlqProducer != nil {
				env := delivery.NewDeadLetter(t, newAttempt, status, errString(doErr), fmt.Sprintf("max attempts reached (%d)", newAttempt))
				b, _ := json.Marshal(env)
				if err := dlqProducer.Publish(cfg.NSQ.DLQTopic, b); err != nil {
					logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithError(err).Error("dlq publish failed")
					tracing.SetSpanError(ctx, err)
				} else {
					logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithField("topic", cfg.NSQ.DLQTopic).Info("dlq published")
					tracing.AddSpanEvent(ctx, "nsq.published_dlq", attribute.String("topic", cfg.NSQ.DLQTopic))
				}
			}

			span.SetAttributes(
				attribute.String("delivery.final_status", "dead"),
				attribute.Int("delivery.final_attempt", newAttempt),
			)

			metrics.RecordDLQ(reason)
			m.Finish() // drop from main topic
			return nil
		}

		// compute backoff with jitter and requeue
		delay := computeDelay(newAttempt, cfg.Worker.BackoffSchedule, cfg.Worker.JitterPercent)
		tracing.AddSpanEvent(ctx, "delivery.requeue",
			attribute.Int("attempt", newAttempt),
			attribute.String("delay", delay.String()),
		)
		span.SetAttributes(
			attribute.String("delivery.final_status", "requeued"),
			attribute.Int("delivery.next_attempt", newAttempt),
		)
		logger.WithContext(ctx).WithDelivery(t.DeliveryID).WithFields(map[string]any{
			"attempt": newAttempt,
			"delay":   delay.String(),
		}).Info("requeue delivery")

		// Update task attempt count before requeuing
		t.Attempt = newAttempt
		updatedBody, _ := json.Marshal(t)
		m.Body = updatedBody

		m.Requeue(delay) // explicit requeue with delay
		return nil
	}))

	// Connecting directly to NSQD forces channel creation, instead of the channel being lazily created on first publish
	if err := consumer.ConnectToNSQD(cfg.NSQ.NsqdTCPAddr); err != nil {
		logger.Plain().WithError(err).Fatal("connect to nsqd failed")
	}

	// Extract host:port from the HTTP URL for NSQ lookupd connection
	lookupAddr := strings.TrimPrefix(cfg.NSQ.LookupHTTPAddr, "http://")
	lookupAddr = strings.TrimPrefix(lookupAddr, "https://")
	if err := consumer.ConnectToNSQLookupd(lookupAddr); err != nil {
		logger.Plain().WithError(err).Fatal("connect to lookupd failed")
	}

	logger.Plain().Info("worker service started")

	// Graceful stop
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	logger.Plain().Info("Shutting down worker service")
	consumer.Stop()
	<-consumer.StopChan
	_ = httpSrv.Shutdown(context.Background())
	logger.Plain().Info("worker service stopped")
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
		errLower := strings.ToLower(doErr.Error())
		if strings.Contains(errLower, "timeout") {
			return "timeout"
		}
		if strings.Contains(errLower, "connection refused") {
			return "connection_refused"
		}
		if strings.Contains(errLower, "no such host") || strings.Contains(errLower, "dns") {
			return "dns_error"
		}
		return "network"
	}
	if status >= 500 {
		return "http_5xx"
	}
	if status == 429 {
		return "http_429"
	}
	if status >= 400 {
		return "http_4xx"
	}
	return "other"
}

// startBacklogMonitor starts a goroutine to periodically update worker backlog metrics
func startBacklogMonitor(cfg config.Config) {
	go func() {
		logger := logging.New("harborhook-worker-monitor")
		ticker := time.NewTicker(15 * time.Second) // Update every 15 seconds
		defer ticker.Stop()

		httpClient := &http.Client{Timeout: 5 * time.Second}

		for range ticker.C {
			// Get NSQ stats from nsqd HTTP endpoint (port 4151)
			nsqdHTTPAddr := strings.Replace(cfg.NSQ.NsqdTCPAddr, ":4150", ":4151", 1)
			resp, err := httpClient.Get(fmt.Sprintf("http://%s/stats?format=json", nsqdHTTPAddr))
			if err != nil {
				logger.Plain().WithError(err).Error("Failed to get NSQ stats")
				continue
			}

			var stats struct {
				Topics []struct {
					Name     string `json:"topic_name"`
					Channels []struct {
						Name  string `json:"channel_name"`
						Depth int64  `json:"depth"`
					} `json:"channels"`
				} `json:"topics"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
				resp.Body.Close()
				logger.Plain().WithError(err).Error("Failed to decode NSQ stats")
				continue
			}
			resp.Body.Close()

			// Update NSQ topic depth metrics only
			for _, topic := range stats.Topics {
				for _, channel := range topic.Channels {
					metrics.UpdateNSQTopicDepth(topic.Name, channel.Name, float64(channel.Depth))
				}
			}
		}
	}()
}
