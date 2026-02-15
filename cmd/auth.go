package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

const (
	dirName             = ".dead-mans-switch"
	credentialsFileName = "credentials.json"
)

// tokenCache stores OAuth2 token data on disk.
type tokenCache struct {
	AccessToken  string `json:"access_token"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
}

// tokenResponse is the raw OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
}

// credentialsDir returns the directory used for storing cached credentials.
func credentialsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, dirName), nil
}

// credentialsPath returns the full path to the cached credentials file.
func credentialsPath() (string, error) {
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}

// saveToken persists a token to the credentials file.
func saveToken(tok *tokenCache) error {
	dir, err := credentialsDir()
	if err != nil {
		return err
	}

	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	path := filepath.Join(dir, "credentials.json")

	err = os.WriteFile(path, data, 0600)
	if err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

// loadToken reads a cached token from disk. Returns nil if no token exists.
func loadToken() (*tokenCache, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	var tok tokenCache
	err = json.Unmarshal(data, &tok)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials file: %w", err)
	}

	return &tok, nil
}

// removeToken deletes the cached credentials file.
func removeToken() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to remove credentials file: %w", err)
	}
	return nil
}

// withBearerToken returns a ClientOption that adds an Authorization header.
func withBearerToken(token string) api.ClientOption {
	return api.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
}

// discoverTokenEndpoint fetches the OIDC discovery document and returns the token_endpoint.
func discoverTokenEndpoint(issuerURL string) (string, error) {
	discoveryURL := strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"

	httpClient := &http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.Get(discoveryURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch OIDC discovery document: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OIDC discovery returned HTTP %d", resp.StatusCode)
	}

	var discovery struct {
		TokenEndpoint string `json:"token_endpoint"`
	}

	err = json.NewDecoder(resp.Body).Decode(&discovery)
	if err != nil {
		return "", fmt.Errorf("failed to parse OIDC discovery document: %w", err)
	}

	if discovery.TokenEndpoint == "" {
		return "", fmt.Errorf("OIDC discovery document missing token_endpoint")
	}

	return discovery.TokenEndpoint, nil
}

// fetchTokenPassword performs the OAuth2 Resource Owner Password Credentials grant.
// Authentik treats this identically to client_credentials, using an app password token.
func fetchTokenPassword(tokenURL, clientID, username, password string) (*tokenResponse, error) {
	data := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {clientID},
		"username":   {username},
		"password":   {password},
		"scope":      {"openid"},
	}

	return doTokenRequest(tokenURL, data)
}

// fetchTokenClientCredentials performs the OAuth2 Client Credentials grant.
func fetchTokenClientCredentials(tokenURL, clientID, clientSecret string) (*tokenResponse, error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"scope":         {"openid"},
	}

	return doTokenRequest(tokenURL, data)
}

// doTokenRequest sends a POST to the token endpoint and parses the response.
func doTokenRequest(tokenURL string, data url.Values) (*tokenResponse, error) {

	httpClient := &http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.PostForm(tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("failed to request token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tok tokenResponse
	err = json.Unmarshal(body, &tok)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	return &tok, nil
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication credentials",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with an OIDC provider and cache the token locally",
	Long: `Authenticate using OAuth2 and cache the resulting token in
~/.dead-mans-switch/credentials.json.

Two grant types are supported:

  Password grant (--username + --password):
    dead-mans-switch auth login \
      --issuer-url URL --client-id ID \
      --username USER --password PASS

  Client credentials grant (--client-secret):
    dead-mans-switch auth login \
      --issuer-url URL --client-id ID \
      --client-secret SECRET

Subsequent CLI commands (e.g. "switch create") will automatically use the
cached token for API requests.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		issuerURL, _ := cmd.Flags().GetString("issuer-url")
		clientID, _ := cmd.Flags().GetString("client-id")
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")
		clientSecret, _ := cmd.Flags().GetString("client-secret")

		// Discover the token endpoint from the OIDC configuration
		tokenURL, err := discoverTokenEndpoint(issuerURL)
		if err != nil {
			return fmt.Errorf("OIDC discovery failed: %w", err)
		}

		var tok *tokenResponse

		switch {
		case clientSecret != "":
			tok, err = fetchTokenClientCredentials(tokenURL, clientID, clientSecret)
		case username != "" && password != "":
			tok, err = fetchTokenPassword(tokenURL, clientID, username, password)
		default:
			return fmt.Errorf("provide either --username and --password, or --client-secret")
		}

		if err != nil {
			return err
		}

		cache := &tokenCache{
			AccessToken:  tok.AccessToken,
			TokenType:    tok.TokenType,
			RefreshToken: tok.RefreshToken,
		}

		if tok.ExpiresIn > 0 {
			cache.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
		}

		err = saveToken(cache)
		if err != nil {
			return err
		}

		if useColor {
			_, _ = color.New(color.FgGreen).Fprintln(cmd.OutOrStdout(), "Login successful — token cached")
		} else {
			cmd.Println("Login successful — token cached")
		}

		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove cached authentication credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := removeToken()
		if err != nil {
			return err
		}

		if useColor {
			_, _ = color.New(color.FgGreen).Fprintln(cmd.OutOrStdout(), "Logged out — cached credentials removed")
		} else {
			cmd.Println("Logged out — cached credentials removed")
		}

		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	RunE: func(cmd *cobra.Command, args []string) error {
		tok, err := loadToken()
		if err != nil {
			return err
		}

		if tok == nil {
			cmd.Println("Not logged in")
			return nil
		}

		status := map[string]string{
			"status":     "logged in",
			"token_type": tok.TokenType,
		}

		if tok.ExpiresAt != "" {
			expiresAt, parseErr := time.Parse(time.RFC3339, tok.ExpiresAt)
			if parseErr == nil {
				if time.Now().After(expiresAt) {
					status["expired"] = "true"
					status["expires_at"] = tok.ExpiresAt
				} else {
					status["expired"] = "false"
					status["expires_at"] = tok.ExpiresAt
				}
			}
		}

		formatOutput(cmd, status, false)
		return nil
	},
}

func init() {
	authCmd.PersistentFlags().BoolVar(&useColor, "color", true, "Enable colorized output")

	authLoginCmd.Flags().String("issuer-url", "", "OIDC issuer URL (e.g. http://localhost:9000/application/o/dead-mans-switch/)")
	authLoginCmd.Flags().String("client-id", "", "OAuth2 client ID")
	authLoginCmd.Flags().String("client-secret", "", "OAuth2 client secret (for client_credentials grant)")
	authLoginCmd.Flags().String("username", "", "Username (for password grant)")
	authLoginCmd.Flags().String("password", "", "Password (for password grant)")

	_ = authLoginCmd.MarkFlagRequired("issuer-url")
	_ = authLoginCmd.MarkFlagRequired("client-id")

	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
