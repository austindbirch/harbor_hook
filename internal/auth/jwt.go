package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TenantContext key for storing tenant ID in context
type contextKey string

const TenantIDKey contextKey = "tenant_id"

// JWTValidator handles JWT token validation
type JWTValidator struct {
	publicKey *rsa.PublicKey
	issuer    string
	audience  string
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(publicKeyPEM, issuer, audience string) (*JWTValidator, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	publicKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		// Try parsing as PKCS8
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %v", err)
		}

		var ok bool
		publicKey, ok = key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key is not RSA")
		}
	}

	return &JWTValidator{
		publicKey: publicKey,
		issuer:    issuer,
		audience:  audience,
	}, nil
}

// ValidateToken validates a JWT token and returns the tenant ID
func (v *JWTValidator) ValidateToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.publicKey, nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to parse token: %v", err)
	}

	if !token.Valid {
		return "", fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims")
	}

	// Validate issuer
	if iss, ok := claims["iss"].(string); !ok || iss != v.issuer {
		return "", fmt.Errorf("invalid issuer")
	}

	// Validate audience
	if aud, ok := claims["aud"].(string); !ok || aud != v.audience {
		return "", fmt.Errorf("invalid audience")
	}

	// Extract tenant ID
	tenantID, ok := claims["tenant_id"].(string)
	if !ok || tenantID == "" {
		return "", fmt.Errorf("missing or invalid tenant_id claim")
	}

	return tenantID, nil
}

// HTTPMiddleware returns an HTTP middleware that validates JWT tokens
func (v *JWTValidator) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health checks and ping endpoints
		if r.URL.Path == "/healthz" || r.URL.Path == "/v1/ping" {
			next.ServeHTTP(w, r)
			return
		}

		// Check for tenant ID header (set by Envoy)
		tenantID := r.Header.Get("x-tenant-id")
		if tenantID != "" {
			// If Envoy already validated and set tenant ID, use it
			ctx := context.WithValue(r.Context(), TenantIDKey, tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Fallback: validate JWT directly
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		tenantID, err := v.ValidateToken(tokenString)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid token: %v", err), http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), TenantIDKey, tenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GRPCInterceptor returns a gRPC unary interceptor that validates JWT tokens
func (v *JWTValidator) GRPCInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Skip auth for health checks
		if strings.Contains(info.FullMethod, "Health") {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
		}

		// Check for tenant ID header (set by Envoy)
		if tenantIDs := md.Get("x-tenant-id"); len(tenantIDs) > 0 {
			ctx = context.WithValue(ctx, TenantIDKey, tenantIDs[0])
			return handler(ctx, req)
		}

		// Fallback: validate JWT directly
		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return nil, status.Errorf(codes.Unauthenticated, "missing authorization header")
		}

		tokenString := strings.TrimPrefix(authHeaders[0], "Bearer ")
		if tokenString == authHeaders[0] {
			return nil, status.Errorf(codes.Unauthenticated, "invalid authorization header format")
		}

		tenantID, err := v.ValidateToken(tokenString)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}

		ctx = context.WithValue(ctx, TenantIDKey, tenantID)
		return handler(ctx, req)
	}
}

// GetTenantIDFromContext extracts tenant ID from context
func GetTenantIDFromContext(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(TenantIDKey).(string)
	return tenantID, ok
}

// JSONWebKeySet represents a JWKS response
type JSONWebKeySet struct {
	Keys []JSONWebKey `json:"keys"`
}

// JSONWebKey represents a single key in JWKS
type JSONWebKey struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// FetchJWKS fetches the JWKS from a URL and returns the public key
func FetchJWKS(jwksURL string) (*rsa.PublicKey, error) {
	resp, err := http.Get(jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %v", err)
	}

	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("no keys found in JWKS")
	}

	// For simplicity, use the first key
	// In production, you'd want to select by kid
	_ = jwks.Keys[0]

	// This is a simplified implementation
	// In practice, you'd need to properly convert JWK to RSA public key
	// For now, we'll return nil and expect the public key to be provided directly
	return nil, fmt.Errorf("JWKS parsing not fully implemented - use direct public key")
}
