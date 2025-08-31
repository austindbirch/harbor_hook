package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	sigHeader = "X-Harborhook-Signature"
	tsHeader  = "X-HarborHook-Timestamp"
)

var (
	failFirstN     = 0
	reqCount       = 0
	endpointSecret = ""
	maxSkew        = 5 * time.Minute
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

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	mux.HandleFunc("/hook", handleHook)

	addr := ":8081"
	log.Printf("fake-receiver listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleHook(w http.ResponseWriter, r *http.Request) {
	reqCount++
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
	if reqCount <= failFirstN {
		log.Printf("FAILING (%d/%d) %s headers=%d body=%s", reqCount, failFirstN, r.URL.Path, len(r.Header), truncate(string(b), 160))
		http.Error(w, "temporary failure", http.StatusInternalServerError)
		return
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
	now := time.Now().Unix()
	if abs64(now-unix) > int64(leeway.Seconds()) {
		return false, "timestamp too far from now (outside leeway)"
	}
	got := strings.TrimPrefix(sigHeaderVal, "sha256=")
	max := hmac.New(sha256.New, []byte(secret))
	max.Write(body)
	max.Write([]byte(ts))
	want := hex.EncodeToString(max.Sum(nil))
	if !hmac.Equal([]byte(got), []byte(want)) {
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
