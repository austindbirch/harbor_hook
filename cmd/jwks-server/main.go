package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

var (
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	keyID      = "harborhook-key-1"
)

// Init attempts to load an existing RSA key pair from env vars. If none found, it generates a new pair
func init() {
	var err error

	// Try to load existing key, else generate new one
	if privateKeyPEM := os.Getenv("JWT_PRIVATE_KEY"); privateKeyPEM != "" {
		block, _ := pem.Decode([]byte(privateKeyPEM))
		if block == nil {
			log.Fatal("Failed to decode PEM private key")
		}

		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			log.Fatalf("Failed to parse private key: %v", err)
		}
	} else {
		// Generate new RSA key pair
		privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatalf("Failed to generate RSA key: %v", err)
		}

		// Store private key for use in other services
		privateKeyDER := x509.MarshalPKCS1PrivateKey(privateKey)
		_ = pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: privateKeyDER,
		})
		log.Printf("Generated new RSA key pair for JWT signing")
	}

	publicKey = &privateKey.PublicKey
}

// jwksHandler serves the JWKS endpoint
func jwksHandler(w http.ResponseWriter, r *http.Request) {
	// Convert RSA public key to JWK format
	jwk := JWK{
		Kty: "RSA",
		Use: "sig",
		Kid: keyID,
		N:   base64UrlEncode(publicKey.N.Bytes()),
		E:   base64UrlEncode(intToBytes(publicKey.E)),
	}

	response := JWKSResponse{
		Keys: []JWK{jwk},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300") // Cache for 5 minutes
	json.NewEncoder(w).Encode(response)
}

// createTokenHandler handles token creation requests
func createTokenHandler(w http.ResponseWriter, r *http.Request) {
	// Parse request
	var req struct {
		TenantID string `json:"tenant_id"`
		TTL      int    `json:"ttl_seconds,omitempty"` // Optional, defaults to 1 hour
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.TenantID == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}

	ttl := req.TTL
	if ttl == 0 {
		ttl = 3600 // Default to 1 hour
	}

	// Create JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":       "harborhook",
		"aud":       "harborhook-api",
		"sub":       req.TenantID,
		"tenant_id": req.TenantID,
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(time.Duration(ttl) * time.Second).Unix(),
	})

	token.Header["kid"] = keyID

	// Sign the token
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		http.Error(w, "Failed to sign token", http.StatusInternalServerError)
		return
	}

	response := map[string]any{
		"token":      tokenString,
		"expires_in": ttl,
		"token_type": "Bearer",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// healthHandler provides a simple health check endpoint
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// main starts the JWKS HTTP server
func main() {
	// Register handlers (jwks, token, health)
	http.HandleFunc("/.well-known/jwks.json", jwksHandler)
	http.HandleFunc("/token", createTokenHandler)
	http.HandleFunc("/healthz", healthHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	log.Printf("JWKS server starting on port %s", port)
	log.Printf("JWKS endpoint: http://localhost:%s/.well-known/jwks.json", port)
	log.Printf("Token creation: POST http://localhost:%s/token", port)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// Helper functions for JWK encoding
func base64UrlEncode(data []byte) string {
	// Base64 URL encode without padding
	encoded := base64.RawURLEncoding.EncodeToString(data)
	return encoded
}

// intToBytes converts an integer to a big-endian byte slice
func intToBytes(i int) []byte {
	if i == 0 {
		return []byte{0}
	}

	bytes := make([]byte, 0)
	for i > 0 {
		bytes = append([]byte{byte(i & 0xff)}, bytes...)
		i >>= 8
	}
	return bytes
}
