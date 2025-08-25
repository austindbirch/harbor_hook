package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nsqio/go-nsq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/austindbirch/harbor_hook/internal/config"
	"github.com/austindbirch/harbor_hook/internal/delivery"
	"github.com/austindbirch/harbor_hook/internal/health"
	"github.com/austindbirch/harbor_hook/internal/metrics"
)

const (
	topic   = "deliveries"
	channel = "workers"
)

func main() {
	cfg := config.FromEnv()
	ctx := context.Background()

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
	mux.HandleFunc("/healthz", health.HTTPHandler(pool))
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

	httpClient := &http.Client{Timeout: 5 * time.Second}

	consumer.AddHandler(nsq.HandlerFunc(func(m *nsq.Message) error {
		var t delivery.Task
		if err := json.Unmarshal(m.Body, &t); err != nil {
			log.Printf("bad task: %v", err)
			metrics.DeliveriesTotal.WithLabelValues("failed").Inc()
			return nil // swallow
		}

		start := time.Now()
		body, _ := json.Marshal(t.Payload)
		req, _ := http.NewRequest(http.MethodPost, t.EndpointURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		latency := time.Since(start)
		status := 0
		if err == nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
		}

		var newStatus string
		if err == nil && status >= 200 && status < 300 {
			newStatus = "ok"
		} else {
			newStatus = "failed"
		}

		// update deliveries row
		_, updErr := pool.Exec(ctx, `
			UPDATE harborhook.deliveries
				SET status=$1, attempt=attempt+1, 
				http_status=$2, latency_ms=$3, 
				updated_at=now(), last_error=$4
			 	WHERE id=$5`,
			newStatus, status, int(latency.Milliseconds()),
			errString(err), t.DeliveryID,
		)
		if updErr != nil {
			log.Printf("db update delivery %s: %v", t.DeliveryID, updErr)
		}

		metrics.DeliveriesTotal.WithLabelValues(newStatus).Inc()
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
