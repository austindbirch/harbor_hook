package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestBase64UrlEncode(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "empty byte slice",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "single byte",
			input:    []byte{0},
			expected: "AA",
		},
		{
			name:     "multiple bytes",
			input:    []byte{1, 2, 3},
			expected: "AQID",
		},
		{
			name:     "text bytes",
			input:    []byte("hello"),
			expected: "aGVsbG8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := base64UrlEncode(tt.input)
			if result != tt.expected {
				t.Errorf("base64UrlEncode(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIntToBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected []byte
	}{
		{
			name:     "zero",
			input:    0,
			expected: []byte{0},
		},
		{
			name:     "single byte value",
			input:    255,
			expected: []byte{255},
		},
		{
			name:     "two byte value",
			input:    256,
			expected: []byte{1, 0},
		},
		{
			name:     "three byte value",
			input:    65536,
			expected: []byte{1, 0, 0},
		},
		{
			name:     "standard RSA exponent",
			input:    65537,
			expected: []byte{1, 0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := intToBytes(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("intToBytes(%d) length = %d, want %d", tt.input, len(result), len(tt.expected))
				return
			}
			for i, b := range result {
				if b != tt.expected[i] {
					t.Errorf("intToBytes(%d) = %v, want %v", tt.input, result, tt.expected)
					break
				}
			}
		})
	}
}

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("healthHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("healthHandler() Content-Type = %q, want %q", contentType, "application/json")
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("healthHandler() failed to unmarshal response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("healthHandler() status = %q, want %q", response["status"], "ok")
	}
}

func TestJwksHandler(t *testing.T) {
	// Set up a test RSA key pair
	testPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test RSA key: %v", err)
	}

	// Temporarily replace the global variables
	originalPrivateKey := privateKey
	originalPublicKey := publicKey
	originalKeyID := keyID
	privateKey = testPrivateKey
	publicKey = &testPrivateKey.PublicKey
	keyID = "test-key-1"
	defer func() {
		privateKey = originalPrivateKey
		publicKey = originalPublicKey
		keyID = originalKeyID
	}()

	req := httptest.NewRequest("GET", "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()

	jwksHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("jwksHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("jwksHandler() Content-Type = %q, want %q", contentType, "application/json")
	}

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "public, max-age=300" {
		t.Errorf("jwksHandler() Cache-Control = %q, want %q", cacheControl, "public, max-age=300")
	}

	var response JWKSResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("jwksHandler() failed to unmarshal response: %v", err)
	}

	if len(response.Keys) != 1 {
		t.Errorf("jwksHandler() keys length = %d, want 1", len(response.Keys))
	}

	jwk := response.Keys[0]
	if jwk.Kty != "RSA" {
		t.Errorf("jwksHandler() key type = %q, want %q", jwk.Kty, "RSA")
	}
	if jwk.Use != "sig" {
		t.Errorf("jwksHandler() key use = %q, want %q", jwk.Use, "sig")
	}
	if jwk.Kid != "test-key-1" {
		t.Errorf("jwksHandler() key id = %q, want %q", jwk.Kid, "test-key-1")
	}
	if jwk.N == "" {
		t.Error("jwksHandler() modulus N is empty")
	}
	if jwk.E == "" {
		t.Error("jwksHandler() exponent E is empty")
	}
}

func TestCreateTokenHandler(t *testing.T) {
	// Set up a test RSA key pair
	testPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test RSA key: %v", err)
	}

	// Temporarily replace the global variables
	originalPrivateKey := privateKey
	originalKeyID := keyID
	privateKey = testPrivateKey
	keyID = "test-key-1"
	defer func() {
		privateKey = originalPrivateKey
		keyID = originalKeyID
	}()

	tests := []struct {
		name               string
		requestBody        string
		expectedStatus     int
		expectedBodyContains string
	}{
		{
			name:               "valid request with tenant_id",
			requestBody:        `{"tenant_id":"test-tenant"}`,
			expectedStatus:     http.StatusOK,
			expectedBodyContains: "token",
		},
		{
			name:               "valid request with ttl",
			requestBody:        `{"tenant_id":"test-tenant","ttl_seconds":7200}`,
			expectedStatus:     http.StatusOK,
			expectedBodyContains: "expires_in",
		},
		{
			name:               "missing tenant_id",
			requestBody:        `{}`,
			expectedStatus:     http.StatusBadRequest,
			expectedBodyContains: "tenant_id is required",
		},
		{
			name:               "empty tenant_id",
			requestBody:        `{"tenant_id":""}`,
			expectedStatus:     http.StatusBadRequest,
			expectedBodyContains: "tenant_id is required",
		},
		{
			name:               "invalid json",
			requestBody:        `{invalid json}`,
			expectedStatus:     http.StatusBadRequest,
			expectedBodyContains: "Invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/token", strings.NewReader(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			createTokenHandler(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("createTokenHandler() status = %d, want %d", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBodyContains) {
				t.Errorf("createTokenHandler() body = %q, want to contain %q", w.Body.String(), tt.expectedBodyContains)
			}

			// For successful cases, validate the JWT structure
			if tt.expectedStatus == http.StatusOK {
				var response map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("createTokenHandler() failed to unmarshal response: %v", err)
				}

				token, ok := response["token"].(string)
				if !ok {
					t.Error("createTokenHandler() token field is not a string")
					return
				}

				expiresIn, ok := response["expires_in"].(float64)
				if !ok {
					t.Error("createTokenHandler() expires_in field is not a number")
				}

				tokenType, ok := response["token_type"].(string)
				if !ok || tokenType != "Bearer" {
					t.Errorf("createTokenHandler() token_type = %q, want %q", tokenType, "Bearer")
				}

				// Verify the JWT can be parsed
				parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
					return &testPrivateKey.PublicKey, nil
				})
				if err != nil {
					t.Errorf("createTokenHandler() generated invalid JWT: %v", err)
					return
				}

				if !parsedToken.Valid {
					t.Error("createTokenHandler() generated invalid JWT")
				}

				claims, ok := parsedToken.Claims.(jwt.MapClaims)
				if !ok {
					t.Error("createTokenHandler() JWT claims are not MapClaims")
					return
				}

				// Validate claims
				if iss, ok := claims["iss"].(string); !ok || iss != "harborhook" {
					t.Errorf("createTokenHandler() issuer = %q, want %q", iss, "harborhook")
				}
				if aud, ok := claims["aud"].(string); !ok || aud != "harborhook-api" {
					t.Errorf("createTokenHandler() audience = %q, want %q", aud, "harborhook-api")
				}

				// Validate TTL if specified
				if strings.Contains(tt.requestBody, "ttl_seconds") && expiresIn != 7200 {
					t.Errorf("createTokenHandler() expires_in = %f, want 7200", expiresIn)
				} else if !strings.Contains(tt.requestBody, "ttl_seconds") && expiresIn != 3600 {
					t.Errorf("createTokenHandler() expires_in = %f, want 3600 (default)", expiresIn)
				}
			}
		})
	}
}

func TestCreateTokenHandler_TTLHandling(t *testing.T) {
	// Set up a test RSA key pair
	testPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test RSA key: %v", err)
	}

	// Temporarily replace the global variables
	originalPrivateKey := privateKey
	originalKeyID := keyID
	privateKey = testPrivateKey
	keyID = "test-key-1"
	defer func() {
		privateKey = originalPrivateKey
		keyID = originalKeyID
	}()

	// Test default TTL (when ttl_seconds is 0 or not provided)
	reqBody := `{"tenant_id":"test-tenant","ttl_seconds":0}`
	req := httptest.NewRequest("POST", "/token", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	createTokenHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("createTokenHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("createTokenHandler() failed to unmarshal response: %v", err)
	}

	expiresIn, ok := response["expires_in"].(float64)
	if !ok || expiresIn != 3600 {
		t.Errorf("createTokenHandler() expires_in = %f, want 3600 (default)", expiresIn)
	}
}

func TestJWKSResponseJSONMarshaling(t *testing.T) {
	jwk := JWK{
		Kty: "RSA",
		Use: "sig",
		Kid: "test-key",
		N:   "test-modulus",
		E:   "test-exponent",
	}

	response := JWKSResponse{
		Keys: []JWK{jwk},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal JWKSResponse: %v", err)
	}

	var unmarshaled JWKSResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal JWKSResponse: %v", err)
	}

	if len(unmarshaled.Keys) != 1 {
		t.Errorf("Unmarshaled keys length = %d, want 1", len(unmarshaled.Keys))
	}

	key := unmarshaled.Keys[0]
	if key.Kty != "RSA" {
		t.Errorf("Unmarshaled kty = %q, want %q", key.Kty, "RSA")
	}
	if key.Use != "sig" {
		t.Errorf("Unmarshaled use = %q, want %q", key.Use, "sig")
	}
	if key.Kid != "test-key" {
		t.Errorf("Unmarshaled kid = %q, want %q", key.Kid, "test-key")
	}
	if key.N != "test-modulus" {
		t.Errorf("Unmarshaled n = %q, want %q", key.N, "test-modulus")
	}
	if key.E != "test-exponent" {
		t.Errorf("Unmarshaled e = %q, want %q", key.E, "test-exponent")
	}
}