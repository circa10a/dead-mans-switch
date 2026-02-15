package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthLogin_Success(t *testing.T) {
	// Spin up a mock token endpoint and OIDC discovery
	mux := http.NewServeMux()
	var tokenServerURL string

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token_endpoint": tokenServerURL + "/token",
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		err := r.ParseForm()
		if err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}

		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %q", r.FormValue("grant_type"))
		}
		if r.FormValue("username") != "testuser" {
			t.Errorf("expected username=testuser, got %q", r.FormValue("username"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "test-access-token-123",
			"token_type":    "Bearer",
			"expires_in":    86400,
			"refresh_token": "test-refresh-token",
		})
	})

	tokenServer := httptest.NewServer(mux)
	tokenServerURL = tokenServer.URL
	defer tokenServer.Close()

	// Use a temp dir for credentials
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	output, err := executeCommand(
		"auth", "login",
		"--issuer-url", tokenServer.URL,
		"--client-id", "test-client",
		"--username", "testuser",
		"--password", "testpass",
		"--color=false",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "Login successful") {
		t.Errorf("expected success message, got %q", output)
	}

	// Verify token was cached
	data, readErr := os.ReadFile(filepath.Join(tmpDir, ".dead-mans-switch", "credentials.json"))
	if readErr != nil {
		t.Fatalf("expected credentials file to exist: %v", readErr)
	}

	var cached tokenCache
	if unmarshalErr := json.Unmarshal(data, &cached); unmarshalErr != nil {
		t.Fatalf("failed to parse credentials: %v", unmarshalErr)
	}
	if cached.AccessToken != "test-access-token-123" {
		t.Errorf("expected access_token %q, got %q", "test-access-token-123", cached.AccessToken)
	}
	if cached.TokenType != "Bearer" {
		t.Errorf("expected token_type %q, got %q", "Bearer", cached.TokenType)
	}
}

func TestAuthLogin_InvalidCredentials(t *testing.T) {
	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token_endpoint": serverURL + "/token",
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid credentials"}`))
	})

	tokenServer := httptest.NewServer(mux)
	serverURL = tokenServer.URL
	defer tokenServer.Close()

	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	_, err := executeCommand(
		"auth", "login",
		"--issuer-url", tokenServer.URL,
		"--client-id", "test-client",
		"--username", "baduser",
		"--password", "badpass",
		"--color=false",
	)

	if err == nil {
		t.Error("expected error for invalid credentials, got nil")
	}
}

func TestAuthLogin_ClientCredentials(t *testing.T) {
	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token_endpoint": serverURL + "/token",
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %q", r.FormValue("grant_type"))
		}
		if r.FormValue("client_secret") != "test-secret" {
			t.Errorf("expected client_secret=test-secret, got %q", r.FormValue("client_secret"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "cc-access-token",
			"token_type":   "Bearer",
			"expires_in":   86400,
		})
	})

	tokenServer := httptest.NewServer(mux)
	serverURL = tokenServer.URL
	defer tokenServer.Close()

	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	output, err := executeCommand(
		"auth", "login",
		"--issuer-url", tokenServer.URL,
		"--client-id", "test-client",
		"--client-secret", "test-secret",
		"--color=false",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "Login successful") {
		t.Errorf("expected success message, got %q", output)
	}

	data, readErr := os.ReadFile(filepath.Join(tmpDir, ".dead-mans-switch", "credentials.json"))
	if readErr != nil {
		t.Fatalf("expected credentials file to exist: %v", readErr)
	}

	var cached tokenCache
	if unmarshalErr := json.Unmarshal(data, &cached); unmarshalErr != nil {
		t.Fatalf("failed to parse credentials: %v", unmarshalErr)
	}
	if cached.AccessToken != "cc-access-token" {
		t.Errorf("expected access_token %q, got %q", "cc-access-token", cached.AccessToken)
	}
}

func TestAuthLogin_NoCredentialsProvided(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token_endpoint": "http://localhost/token",
		})
	})

	tokenServer := httptest.NewServer(mux)
	defer tokenServer.Close()

	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	_, err := executeCommand(
		"auth", "login",
		"--issuer-url", tokenServer.URL,
		"--client-id", "test-client",
		"--color=false",
	)

	if err == nil {
		t.Error("expected error when no credentials provided")
	}
}

func TestAuthLogout(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	// Create a fake credentials file
	credDir := filepath.Join(tmpDir, ".dead-mans-switch")
	err := os.MkdirAll(credDir, 0o700)
	if err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(credDir, "credentials.json"), []byte(`{"access_token":"old"}`), 0o600)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	output, execErr := executeCommand("auth", "logout", "--color=false")
	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}
	if !strings.Contains(output, "Logged out") {
		t.Errorf("expected logout message, got %q", output)
	}

	// Verify file was removed
	if _, statErr := os.Stat(filepath.Join(credDir, "credentials.json")); !os.IsNotExist(statErr) {
		t.Error("expected credentials file to be removed")
	}
}

func TestAuthLogout_NoCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	output, err := executeCommand("auth", "logout", "--color=false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "Logged out") {
		t.Errorf("expected logout message, got %q", output)
	}
}

func TestAuthStatus_NotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	output, err := executeCommand("auth", "status", "--color=false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "Not logged in") {
		t.Errorf("expected 'Not logged in', got %q", output)
	}
}

func TestAuthStatus_LoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	credDir := filepath.Join(tmpDir, ".dead-mans-switch")
	err := os.MkdirAll(credDir, 0o700)
	if err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(credDir, "credentials.json"),
		[]byte(`{"access_token":"tok123","token_type":"Bearer","expires_at":"2099-01-01T00:00:00Z"}`), 0o600)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	output, execErr := executeCommand("auth", "status", "--color=false")
	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}
	if !strings.Contains(output, "logged in") {
		t.Errorf("expected 'logged in' status, got %q", output)
	}
}

func TestTokenCacheRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	original := &tokenCache{
		AccessToken:  "my-token",
		TokenType:    "Bearer",
		RefreshToken: "my-refresh",
		ExpiresAt:    "2099-12-31T23:59:59Z",
	}

	err := saveToken(original)
	if err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	loaded, err := loadToken()
	if err != nil {
		t.Fatalf("failed to load token: %v", err)
	}

	if loaded.AccessToken != original.AccessToken {
		t.Errorf("expected access_token %q, got %q", original.AccessToken, loaded.AccessToken)
	}
	if loaded.TokenType != original.TokenType {
		t.Errorf("expected token_type %q, got %q", original.TokenType, loaded.TokenType)
	}
	if loaded.RefreshToken != original.RefreshToken {
		t.Errorf("expected refresh_token %q, got %q", original.RefreshToken, loaded.RefreshToken)
	}
	if loaded.ExpiresAt != original.ExpiresAt {
		t.Errorf("expected expires_at %q, got %q", original.ExpiresAt, loaded.ExpiresAt)
	}
}

func TestLoadToken_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	tok, err := loadToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != nil {
		t.Error("expected nil token when no file exists")
	}
}

func TestInitClient_WithCachedToken(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { t.Setenv("HOME", origHome) }()

	// Cache a token
	err := saveToken(&tokenCache{
		AccessToken: "cached-token-xyz",
		TokenType:   "Bearer",
	})
	if err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	// Set up a mock server that checks for the auth header
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	apiURL = server.URL
	err = initClient()
	if err != nil {
		t.Fatalf("failed to init client: %v", err)
	}

	_, err = client.GetSwitchWithResponse(t.Context(), nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if receivedAuth != "Bearer cached-token-xyz" {
		t.Errorf("expected Authorization header %q, got %q", "Bearer cached-token-xyz", receivedAuth)
	}
}

func TestFetchTokenPassword_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "fetched-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	tok, err := fetchTokenPassword(server.URL, "client-id", "user", "pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.AccessToken != "fetched-token" {
		t.Errorf("expected access_token %q, got %q", "fetched-token", tok.AccessToken)
	}
}

func TestFetchTokenClientCredentials_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %q", r.FormValue("grant_type"))
		}
		if r.FormValue("client_secret") != "my-secret" {
			t.Errorf("expected client_secret=my-secret, got %q", r.FormValue("client_secret"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "cc-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	tok, err := fetchTokenClientCredentials(server.URL, "client-id", "my-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.AccessToken != "cc-token" {
		t.Errorf("expected access_token %q, got %q", "cc-token", tok.AccessToken)
	}
}

func TestFetchToken_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	_, err := fetchTokenPassword(server.URL, "client-id", "user", "pass")
	if err == nil {
		t.Error("expected error for server error response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status code, got %q", err.Error())
	}
}
