package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTAuth(t *testing.T) {
	// Generate RSA key pair for testing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := &privateKey.PublicKey

	issuerURL := "https://authentik.example.com/application/o/oauth2/"
	audience := "dead-mans-switch"

	publicKeys := map[string]*rsa.PublicKey{
		"test-key": publicKey,
	}

	tests := []struct {
		name           string
		enabled        bool
		header         string
		tokenFunc      func() string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "auth disabled",
			enabled:        false,
			header:         "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing authorization header",
			enabled:        true,
			header:         "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Missing authorization header",
		},
		{
			name:           "invalid authorization header format",
			enabled:        true,
			header:         "InvalidFormat",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid authorization header",
		},
		{
			name:    "valid token",
			enabled: true,
			header: func() string {
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
					Sub:   "user123",
					Name:  "Test User",
					Email: "test@example.com",
					RegisteredClaims: jwt.RegisteredClaims{
						Issuer:    issuerURL,
						Audience:  jwt.ClaimStrings{audience},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
					},
				})
				token.Header["kid"] = "test-key"

				tokenString, err := token.SignedString(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				return "Bearer " + tokenString
			}(),
			expectedStatus: http.StatusOK,
		},
		{
			name:    "expired token",
			enabled: true,
			header: func() string {
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Issuer:    issuerURL,
						Audience:  jwt.ClaimStrings{audience},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
					},
				})
				token.Header["kid"] = "test-key"

				tokenString, err := token.SignedString(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				return "Bearer " + tokenString
			}(),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid token",
		},
		{
			name:    "invalid issuer",
			enabled: true,
			header: func() string {
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Issuer:    "https://wrong-issuer.example.com",
						Audience:  jwt.ClaimStrings{audience},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
					},
				})
				token.Header["kid"] = "test-key"

				tokenString, err := token.SignedString(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				return "Bearer " + tokenString
			}(),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid issuer",
		},
		{
			name:    "invalid audience",
			enabled: true,
			header: func() string {
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Issuer:    issuerURL,
						Audience:  jwt.ClaimStrings{"wrong-audience"},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
					},
				})
				token.Header["kid"] = "test-key"

				tokenString, err := token.SignedString(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				return "Bearer " + tokenString
			}(),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid audience",
		},
		{
			name:    "missing kid in token",
			enabled: true,
			header: func() string {
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Issuer:    issuerURL,
						Audience:  jwt.ClaimStrings{audience},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
					},
				})

				tokenString, err := token.SignedString(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				return "Bearer " + tokenString
			}(),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid token",
		},
		{
			name:    "unknown kid",
			enabled: true,
			header: func() string {
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Issuer:    issuerURL,
						Audience:  jwt.ClaimStrings{audience},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
					},
				})
				token.Header["kid"] = "unknown-key"

				tokenString, err := token.SignedString(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				return "Bearer " + tokenString
			}(),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid token",
		},
		{
			name:    "issuer URL trailing slash normalization",
			enabled: true,
			header: func() string {
				// Token issuer without trailing slash should match validator with trailing slash
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
					Sub: "user123",
					RegisteredClaims: jwt.RegisteredClaims{
						Issuer:    strings.TrimSuffix(issuerURL, "/"),
						Audience:  jwt.ClaimStrings{audience},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
					},
				})
				token.Header["kid"] = "test-key"

				tokenString, err := token.SignedString(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				return "Bearer " + tokenString
			}(),
			expectedStatus: http.StatusOK,
		},
		{
			name:    "missing user identifier in token",
			enabled: true,
			header: func() string {
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
					Sub:   "",
					Name:  "",
					Email: "",
					RegisteredClaims: jwt.RegisteredClaims{
						Issuer:    issuerURL,
						Audience:  jwt.ClaimStrings{audience},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
					},
				})
				token.Header["kid"] = "test-key"

				tokenString, err := token.SignedString(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				return "Bearer " + tokenString
			}(),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "No user identifier in token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &JWTValidator{
				Enabled:    tt.enabled,
				IssuerURL:  issuerURL,
				Audience:   audience,
				PublicKeys: publicKeys,
			}

			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			})

			handler := JWTAuth(v)(nextHandler)

			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("unexpected status code: expected %d, got %d", tt.expectedStatus, w.Code)
			}
			if tt.expectedBody != "" {
				if !strings.Contains(w.Body.String(), tt.expectedBody) {
					t.Errorf("unexpected response body: expected to contain %q, got %q", tt.expectedBody, w.Body.String())
				}
			}
		})
	}
}

func TestJWTAuthWithoutAudience(t *testing.T) {
	// Generate RSA key pair for testing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := &privateKey.PublicKey

	issuerURL := "https://authentik.example.com/application/o/oauth2/"

	publicKeys := map[string]*rsa.PublicKey{
		"test-key": publicKey,
	}

	// Without audience validation
	validator := &JWTValidator{
		Enabled:    true,
		IssuerURL:  issuerURL,
		Audience:   "", // Empty audience
		PublicKeys: publicKeys,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &customClaims{
		Sub: "user123",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuerURL,
			Audience:  jwt.ClaimStrings{"any-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	token.Header["kid"] = "test-key"

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	handler := JWTAuth(validator)(nextHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("should pass when audience is empty: expected %d, got %d", http.StatusOK, w.Code)
	}
}

func TestFetchPublicKeys(t *testing.T) {
	tests := []struct {
		name           string
		mockHandler    http.HandlerFunc
		expectedError  bool
		expectedKeyNum int
	}{
		{
			name: "valid JWKS response",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/openid-configuration" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]string{
						"jwks_uri": "http://" + r.Host + "/jwks",
					})
					return
				}
				if r.URL.Path == "/jwks" {
					jwks := jsonWebKeySet{
						Keys: []jsonWebKey{
							{
								Kty: "RSA",
								Kid: "key1",
								Use: "sig",
								N:   "xjlCRBqkQRY-nP0YfRNIoRMhjdHd5BzuFxCvEPdSWJXFBnI...",
								E:   "AQAB",
								Alg: "RS256",
							},
						},
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(jwks)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			expectedError: true, // Will fail due to invalid key values, but tests the flow
		},
		{
			name: "non-200 status on discovery",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectedError: true,
		},
		{
			name: "invalid JSON on discovery",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("invalid json"))
			},
			expectedError: true,
		},
		{
			name: "discovery missing jwks_uri",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{
					"issuer": "http://example.com",
				})
			},
			expectedError: true,
		},
		{
			name: "non-200 status on JWKS",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/openid-configuration" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]string{
						"jwks_uri": "http://" + r.Host + "/jwks",
					})
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedError: true,
		},
		{
			name: "invalid JSON on JWKS",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/openid-configuration" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]string{
						"jwks_uri": "http://" + r.Host + "/jwks",
					})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("invalid json"))
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.mockHandler)
			defer server.Close()

			_, err := FetchPublicKeys(server.URL)
			if tt.expectedError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDecodeBase64URL(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr bool
		checkOutput func([]byte) bool
	}{
		{
			name:        "valid base64url without padding",
			input:       "SGVsbG8gV29ybGQ",
			expectedErr: false,
			checkOutput: func(b []byte) bool {
				return string(b) == "Hello World"
			},
		},
		{
			name:        "valid base64url with padding",
			input:       "SGVsbG8",
			expectedErr: false,
			checkOutput: func(b []byte) bool {
				return string(b) == "Hello"
			},
		},
		{
			name:        "invalid base64url",
			input:       "!!!invalid!!!",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeBase64URL(tt.input)
			if tt.expectedErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !tt.checkOutput(result) {
					t.Error("output check failed")
				}
			}
		})
	}
}

func TestJWKToRSAPublicKey(t *testing.T) {
	tests := []struct {
		name        string
		jwk         jsonWebKey
		expectedErr bool
	}{
		{
			name:        "non-RSA key",
			jwk:         jsonWebKey{Kty: "EC"},
			expectedErr: true,
		},
		{
			name: "invalid modulus encoding",
			jwk: jsonWebKey{
				Kty: "RSA",
				N:   "!!!invalid!!!",
				E:   "AQAB",
			},
			expectedErr: true,
		},
		{
			name: "invalid exponent encoding",
			jwk: jsonWebKey{
				Kty: "RSA",
				N:   "xjlCRBqkQRY",
				E:   "!!!invalid!!!",
			},
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := jwkToRSAPublicKey(tt.jwk)
			if tt.expectedErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectedErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
