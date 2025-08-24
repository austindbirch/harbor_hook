package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/austindbirch/harbor_hook/internal/config"
	"github.com/austindbirch/harbor_hook/internal/health"
	"github.com/nsqio/go-nsq"
)

func main() {
	cfg := config.FromEnv()

	var lastOK atomic.Value
	lastOK.Store(time.Now())

	prod, err := nsq.NewProducer(cfg.NSQ.NsqdTCPAddr, nsq.NewConfig())
	if err != nil {
		log.Fatalf("nsq producer: %v", err)
	}
	defer prod.Stop()

	// Simple background ping (publish to a throwaway topic)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := prod.Ping(); err != nil {
					log.Printf("nsq ping failed: %v", err)
				} else {
					lastOK.Store(time.Now())
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// HTTP health: ok if we've pinged NSQ within last minute
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		t := lastOK.Load().(time.Time)
		ok := time.Since(t) < time.Minute
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		health.HTTPHandler(nil).ServeHTTP(w, r)
	})
	srv := &http.Server{Addr: ":8082"}
	go func() {
		log.Printf("worker HTTP listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("worker http serve: %v", err)
		}
	}()

	// Graceful stop
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	_ = srv.Shutdown(context.Background())
	cancel()
	log.Println("worker stopped")
}
