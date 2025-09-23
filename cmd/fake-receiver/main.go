package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/austindbirch/harbor_hook/internal/config"
)

var (
	reqCount = atomic.Int64{}
)

func main() {
	cfg := config.FromEnv()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	mux.HandleFunc("/hook", handleHookFactory(cfg))

	server := &http.Server{
		Addr:         cfg.FakeReceiver.Port,
		Handler:      mux,
		ReadTimeout:  cfg.FakeReceiver.ReadTimeout,
		WriteTimeout: cfg.FakeReceiver.WriteTimeout,
		IdleTimeout:  cfg.FakeReceiver.IdleTimeout,
	}
	log.Printf("fake-receiver listening on %s", cfg.FakeReceiver.Port)
	log.Fatal(server.ListenAndServe())
}

func handleHookFactory(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleHook(w, r, cfg)
	}
}

func handleHook(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	n := reqCount.Add(1)
	b, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	if cfg.FakeReceiver.EndpointSecret != "" {
		leeway := time.Duration(cfg.FakeReceiver.SigningLeewaySeconds) * time.Second
		if ok, msg := verifySignature(cfg.FakeReceiver.EndpointSecret, b, r.Header.Get(cfg.NSQ.TimestampHeader), r.Header.Get(cfg.NSQ.SignatureHeader), leeway); !ok {
			traceID := r.Header.Get("X-Trace-Id")
			if traceID != "" {
				log.Printf("fake-receiver failed to verify signature: %s trace_id=%s", msg, traceID)
			} else {
				log.Printf("fake-receiver failed to verify signature: %s", msg)
			}
			http.Error(w, "invalid signature: "+msg, http.StatusUnauthorized)
			return
		}
	}

	// Simulate flakiness: first N request -> 500
	if n <= int64(cfg.FakeReceiver.FailFirstN) {
		traceID := r.Header.Get("X-Trace-Id")
		if traceID != "" {
			log.Printf("FAILING (%d/%d) %s trace_id=%s headers=%d body=%s", n, cfg.FakeReceiver.FailFirstN, r.URL.Path, traceID, len(r.Header), truncate(string(b), 160))
		} else {
			log.Printf("FAILING (%d/%d) %s headers=%d body=%s", n, cfg.FakeReceiver.FailFirstN, r.URL.Path, len(r.Header), truncate(string(b), 160))
		}
		http.Error(w, "temporary failure", http.StatusInternalServerError)
		return
	}

	// Simulate processing delay if configured
	responseDelay := time.Duration(cfg.FakeReceiver.ResponseDelayMS) * time.Millisecond
	if responseDelay > 0 {
		time.Sleep(responseDelay)
	}

	// Extract trace ID for end-to-end traceability
	traceID := r.Header.Get("X-Trace-Id")
	if traceID != "" {
		log.Printf("fake-receiver OK %s trace_id=%s headers=%d body=%q", r.URL.Path, traceID, len(r.Header), truncate(string(b), 160))
	} else {
		log.Printf("fake-receiver OK %s headers=%d body=%q", r.URL.Path, len(r.Header), truncate(string(b), 160))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`ok`))
}

func verifySignature(secret string, body []byte, ts, sigHeaderVal string, leeway time.Duration) (bool, string) {
	if ts == "" || sigHeaderVal == "" {
		return false, "missing headers"
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false, "invalid timestamp"
	}
	// reject if timestamp is too old/new
	if abs64(time.Now().Unix()-unix) > int64(leeway.Seconds()) {
		return false, "timestamp outside leeway"
	}

	// expect "sha256=<hex>"
	parts := strings.SplitN(sigHeaderVal, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return false, "bad signature scheme"
	}
	gotSig, err := hex.DecodeString(parts[1])
	if err != nil {
		return false, "signature not hex"
	}

	// Compute HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	mac.Write([]byte(ts))
	want := mac.Sum(nil)

	// Check got against want
	if len(gotSig) != len(want) || subtle.ConstantTimeCompare(gotSig, want) != 1 {
		return false, "sig mismatch"
	}
	return true, ""
}

// abs64 returns the absolute value of an int64
func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// truncate truncates a string to the specified length and adds an ellipsis if truncated
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return fmt.Sprintf("%s...", s[:n])
}
