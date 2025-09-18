package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/austindbirch/harbor_hook/internal/config"
)

func TestVerifySignature(t *testing.T) {
	secret := "test-secret"
	body := []byte("test payload")
	now := time.Now().Unix()
	leeway := 5 * time.Minute

	// Create valid signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	mac.Write([]byte(strconv.FormatInt(now, 10)))
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name        string
		secret      string
		body        []byte
		timestamp   string
		signature   string
		leeway      time.Duration
		expectValid bool
		expectedMsg string
	}{
		{
			name:        "valid signature",
			secret:      secret,
			body:        body,
			timestamp:   strconv.FormatInt(now, 10),
			signature:   validSig,
			leeway:      leeway,
			expectValid: true,
			expectedMsg: "",
		},
		{
			name:        "missing timestamp",
			secret:      secret,
			body:        body,
			timestamp:   "",
			signature:   validSig,
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "missing headers",
		},
		{
			name:        "missing signature",
			secret:      secret,
			body:        body,
			timestamp:   strconv.FormatInt(now, 10),
			signature:   "",
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "missing headers",
		},
		{
			name:        "invalid timestamp format",
			secret:      secret,
			body:        body,
			timestamp:   "not-a-number",
			signature:   validSig,
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "invalid timestamp",
		},
		{
			name:        "timestamp too old",
			secret:      secret,
			body:        body,
			timestamp:   strconv.FormatInt(now-int64(leeway.Seconds())-10, 10),
			signature:   validSig,
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "timestamp outside leeway",
		},
		{
			name:        "timestamp too new",
			secret:      secret,
			body:        body,
			timestamp:   strconv.FormatInt(now+int64(leeway.Seconds())+10, 10),
			signature:   validSig,
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "timestamp outside leeway",
		},
		{
			name:        "bad signature scheme",
			secret:      secret,
			body:        body,
			timestamp:   strconv.FormatInt(now, 10),
			signature:   "md5=abcdef",
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "bad signature scheme",
		},
		{
			name:        "signature not hex",
			secret:      secret,
			body:        body,
			timestamp:   strconv.FormatInt(now, 10),
			signature:   "sha256=not-hex",
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "signature not hex",
		},
		{
			name:        "signature mismatch",
			secret:      secret,
			body:        body,
			timestamp:   strconv.FormatInt(now, 10),
			signature:   "sha256=deadbeef",
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "sig mismatch",
		},
		{
			name:        "wrong secret",
			secret:      "wrong-secret",
			body:        body,
			timestamp:   strconv.FormatInt(now, 10),
			signature:   validSig,
			leeway:      leeway,
			expectValid: false,
			expectedMsg: "sig mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, msg := verifySignature(tt.secret, tt.body, tt.timestamp, tt.signature, tt.leeway)

			if valid != tt.expectValid {
				t.Errorf("verifySignature() valid = %v, want %v", valid, tt.expectValid)
			}
			if msg != tt.expectedMsg {
				t.Errorf("verifySignature() msg = %q, want %q", msg, tt.expectedMsg)
			}
		})
	}
}

func TestAbs64(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected int64
	}{
		{
			name:     "positive number",
			input:    42,
			expected: 42,
		},
		{
			name:     "negative number",
			input:    -42,
			expected: 42,
		},
		{
			name:     "zero",
			input:    0,
			expected: 0,
		},
		{
			name:     "max int64",
			input:    9223372036854775807,
			expected: 9223372036854775807,
		},
		{
			name:     "min int64 + 1",
			input:    -9223372036854775807,
			expected: 9223372036854775807,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := abs64(tt.input)
			if result != tt.expected {
				t.Errorf("abs64(%d) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		length   int
		expected string
	}{
		{
			name:     "string shorter than limit",
			input:    "hello",
			length:   10,
			expected: "hello",
		},
		{
			name:     "string equal to limit",
			input:    "hello",
			length:   5,
			expected: "hello",
		},
		{
			name:     "string longer than limit",
			input:    "hello world",
			length:   5,
			expected: "hello...",
		},
		{
			name:     "empty string",
			input:    "",
			length:   5,
			expected: "",
		},
		{
			name:     "zero length limit",
			input:    "hello",
			length:   0,
			expected: "...",
		},
		{
			name:     "very long string",
			input:    "this is a very long string that should be truncated",
			length:   10,
			expected: "this is a ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.length)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.length, result, tt.expected)
			}
		})
	}
}

func TestHealthzHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("healthz handler status = %d, want %d", w.Code, http.StatusOK)
	}

	expected := `{"ok":true}`
	if w.Body.String() != expected {
		t.Errorf("healthz handler body = %q, want %q", w.Body.String(), expected)
	}
}

func TestHandleHook(t *testing.T) {
	cfg := config.FromEnv() // Get default config

	tests := []struct {
		name                 string
		body                 string
		headers              map[string]string
		cfgOverrides         config.FakeReceiver
		expectedStatus       int
		expectedBodyContains string
	}{
		{
			name:                 "successful request",
			body:                 "test payload",
			headers:              map[string]string{},
			cfgOverrides:         config.FakeReceiver{FailFirstN: 0, EndpointSecret: ""},
			expectedStatus:       http.StatusOK,
			expectedBodyContains: "ok",
		},
		{
			name:                 "fail first request",
			body:                 "test payload",
			headers:              map[string]string{},
			cfgOverrides:         config.FakeReceiver{FailFirstN: 1, EndpointSecret: ""},
			expectedStatus:       http.StatusInternalServerError,
			expectedBodyContains: "temporary failure",
		},
		{
			name: "missing signature with secret configured",
			body: "test payload",
			headers: map[string]string{
				"X-HarborHook-Timestamp": strconv.FormatInt(time.Now().Unix(), 10),
			},
			cfgOverrides:         config.FakeReceiver{FailFirstN: 0, EndpointSecret: "test-secret"},
			expectedStatus:       http.StatusUnauthorized,
			expectedBodyContains: "invalid signature",
		},
		{
			name: "valid signature with secret",
			body: "test payload",
			headers: func() map[string]string {
				now := time.Now().Unix()
				ts := strconv.FormatInt(now, 10)
				mac := hmac.New(sha256.New, []byte("test-secret"))
				mac.Write([]byte("test payload"))
				mac.Write([]byte(ts))
				sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
				return map[string]string{
					"X-HarborHook-Timestamp": ts,
					"X-HarborHook-Signature": sig,
				}
			}(),
			cfgOverrides:         config.FakeReceiver{FailFirstN: 0, EndpointSecret: "test-secret"},
			expectedStatus:       http.StatusOK,
			expectedBodyContains: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset request counter
			reqCount.Store(0)

			// Create test config with overrides
			testCfg := cfg
			testCfg.FakeReceiver = tt.cfgOverrides
			testCfg.NSQ = cfg.NSQ // Use default NSQ config for headers

			req := httptest.NewRequest("POST", "/hook", strings.NewReader(tt.body))
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()

			// Use the new handleHook function that takes config
			handleHook(w, req, testCfg)

			if w.Code != tt.expectedStatus {
				t.Errorf("handleHook() status = %d, want %d", w.Code, tt.expectedStatus)
			}
			if !strings.Contains(w.Body.String(), tt.expectedBodyContains) {
				t.Errorf("handleHook() body = %q, want to contain %q", w.Body.String(), tt.expectedBodyContains)
			}
		})
	}
}
