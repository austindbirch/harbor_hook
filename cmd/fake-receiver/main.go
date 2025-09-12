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
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	sigHeader = "X-Harborhook-Signature"
	tsHeader  = "X-HarborHook-Timestamp"
)

var (
	failFirstN     = 0
	reqCount       = atomic.Int64{}
	endpointSecret = ""
	maxSkew        = 5 * time.Minute
	responseDelay  = 0 * time.Millisecond
)

func main() {
	// Parse fail first settings
	if v := os.Getenv("FAIL_FIRST_N"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			failFirstN = n
		}
	}
	// Parse endpoint secret
	if v := os.Getenv("ENDPOINT_SECRET"); v != "" {
		endpointSecret = v
	}
	// Parse signing timestamp leeway
	if v := os.Getenv("SIGNING_LEEWAY_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxSkew = time.Duration(n) * time.Second
		}
	}
	// Parse response delay for load simulation
	if v := os.Getenv("RESPONSE_DELAY_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			responseDelay = time.Duration(n) * time.Millisecond
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	mux.HandleFunc("/hook", handleHook)

	addr := ":8081"
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("fake-receiver listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

func handleHook(w http.ResponseWriter, r *http.Request) {
	n := reqCount.Add(1)
	b, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	if endpointSecret != "" {
		if ok, msg := verifySignature(endpointSecret, b, r.Header.Get(tsHeader), r.Header.Get(sigHeader), maxSkew); !ok {
			log.Printf("fake-receiver failed to verify signature: %s", msg)
			http.Error(w, "invalid signature: "+msg, http.StatusUnauthorized)
			return
		}
	}

	// Simulate flakiness: first N request -> 500
	if n <= int64(failFirstN) {
		log.Printf("FAILING (%d/%d) %s headers=%d body=%s", n, failFirstN, r.URL.Path, len(r.Header), truncate(string(b), 160))
		http.Error(w, "temporary failure", http.StatusInternalServerError)
		return
	}

	// Simulate processing delay if configured
	if responseDelay > 0 {
		time.Sleep(responseDelay)
	}

	log.Printf("fake-receiver OK %s  headers=%d body=%q", r.URL.Path, len(r.Header), truncate(string(b), 160))
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
