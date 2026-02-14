package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

const (
	UserIDKey contextKey = "user_id"
	adminUser string     = "admin"
)

// GetUserIDFromContext retrieves the user ID from the request context
func GetUserIDFromContext(r *http.Request) string {
	userID := r.Context().Value(UserIDKey)
	if userID != nil {
		return userID.(string)
	}
	return adminUser
}

// JWTValidator holds configuration for JWT validation
type JWTValidator struct {
	Audience   string
	Enabled    bool
	IssuerURL  string
	PublicKeys map[string]*rsa.PublicKey
}

// jsonWebKeySet represents a JWKS response
type jsonWebKeySet struct {
	Keys []jsonWebKey `json:"keys"`
}

// jsonWebKey represents a single key in JWKS
type jsonWebKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

// customClaims represents JWT claims from Authentik
type customClaims struct {
	Sub   string `json:"sub"`
	Name  string `json:"name"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// JWTAuth is a middleware that validates JWT tokens from Authentik
func JWTAuth(validator *JWTValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validator.Enabled {
				// Auth disabled - set default user ID
				ctx := context.WithValue(r.Context(), UserIDKey, adminUser)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
				return
			}

			tokenString := parts[1]

			// Parse and validate token
			token, err := jwt.ParseWithClaims(tokenString, &customClaims{}, func(token *jwt.Token) (interface{}, error) {
				// Verify signing method
				if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}

				kid, ok := token.Header["kid"].(string)
				if !ok {
					return nil, fmt.Errorf("missing kid in token header")
				}

				// Get public key
				publicKey, ok := validator.PublicKeys[kid]
				if !ok {
					return nil, fmt.Errorf("public key not found for kid: %s", kid)
				}

				return publicKey, nil
			})

			if err != nil {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			if !token.Valid {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Validate claims
			claims, ok := token.Claims.(*customClaims)
			if !ok {
				http.Error(w, "Invalid token claims", http.StatusUnauthorized)
				return
			}

			// Verify issuer
			if claims.Issuer != validator.IssuerURL {
				http.Error(w, "Invalid issuer", http.StatusUnauthorized)
				return
			}

			// Verify audience if configured
			if validator.Audience != "" {
				hasAudience := false
				for _, aud := range claims.Audience {
					if aud == validator.Audience {
						hasAudience = true
						break
					}
				}
				if !hasAudience {
					http.Error(w, "Invalid audience", http.StatusUnauthorized)
					return
				}
			}

			// Extract user ID from subject claim (or email/username if available)
			userID := claims.Sub
			if userID == "" && claims.Email != "" {
				userID = claims.Email
			}
			if userID == "" && claims.Name != "" {
				userID = claims.Name
			}
			if userID == "" {
				http.Error(w, "No user identifier in token", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// oidcDiscovery represents the OpenID Connect discovery document
type oidcDiscovery struct {
	JWKSURI string `json:"jwks_uri"`
}

// FetchPublicKeys fetches JWKS from the issuer's OIDC discovery endpoint
func FetchPublicKeys(issuerURL string) (map[string]*rsa.PublicKey, error) {
	// Use OIDC discovery to find the JWKS URI
	discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"

	parsedDiscoveryURL, err := url.ParseRequestURI(discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("invalid OIDC discovery URL: %w", err)
	}

	discoveryReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, parsedDiscoveryURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC discovery request: %w", err)
	}

	discoveryResp, err := http.DefaultClient.Do(discoveryReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC discovery: %w", err)
	}
	defer func() { _ = discoveryResp.Body.Close() }()

	if discoveryResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery endpoint returned status %d", discoveryResp.StatusCode)
	}

	var discovery oidcDiscovery
	if err := json.NewDecoder(discoveryResp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("failed to decode OIDC discovery: %w", err)
	}

	if discovery.JWKSURI == "" {
		return nil, fmt.Errorf("OIDC discovery did not contain jwks_uri")
	}

	parsedJWKSURL, err := url.ParseRequestURI(discovery.JWKSURI)
	if err != nil {
		return nil, fmt.Errorf("invalid JWKS URL: %w", err)
	}

	jwksReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, parsedJWKSURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS request: %w", err)
	}

	resp, err := http.DefaultClient.Do(jwksReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks jsonWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	publicKeys := make(map[string]*rsa.PublicKey)

	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}

		publicKey, err := jwkToRSAPublicKey(key)
		if err != nil {
			continue
		}

		publicKeys[key.Kid] = publicKey
	}

	if len(publicKeys) == 0 {
		return nil, fmt.Errorf("no valid RSA keys found in JWKS")
	}

	return publicKeys, nil
}

// jwkToRSAPublicKey converts a JWK to an RSA public key
func jwkToRSAPublicKey(jwk jsonWebKey) (*rsa.PublicKey, error) {
	if jwk.Kty != "RSA" {
		return nil, fmt.Errorf("unsupported key type: %s", jwk.Kty)
	}

	nBytes, err := decodeBase64URL(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	eBytes, err := decodeBase64URL(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	e := 0
	for _, b := range eBytes {
		e = e*256 + int(b)
	}

	n := new(big.Int)
	n.SetBytes(nBytes)

	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}

// decodeBase64URL decodes a base64url-encoded string
func decodeBase64URL(s string) ([]byte, error) {
	// Add padding if necessary
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}

	return base64.URLEncoding.DecodeString(s)
}
