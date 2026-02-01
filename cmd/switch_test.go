package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/stretchr/testify/assert"
)

// executeCommand is a helper to run cobra commands and capture output
func executeCommand(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)

	err := rootCmd.Execute()
	return buf.String(), err
}

func Test_CreateCommand(t *testing.T) {
	// Setup a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/switch", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var body api.Switch
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "test-message", body.Message)

		// Return a 201 Created with a Switch object
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		id := 1
		_ = json.NewEncoder(w).Encode(api.Switch{
			Id:      &id,
			Message: body.Message,
		})
	}))
	defer server.Close()

	output, err := executeCommand("switch", "create", "-m", "test-message", "-n", "logger://", "--url", server.URL, "--color=false")

	// 3. Assertions
	assert.NoError(t, err)
	assert.Contains(t, output, `"id": 1`)
	assert.Contains(t, output, `"message": "test-message"`)
}

func Test_GetCommand_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(api.Error{
			Code:    404,
			Message: "switch not found",
		})
	}))
	defer server.Close()

	output, err := executeCommand("switch", "get", "999", "--url", server.URL, "--color=false")

	assert.NoError(t, err)
	assert.Contains(t, output, `"message": "switch not found"`)
}

func Test_ResetCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/switch/1/reset", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		id := 1
		_ = json.NewEncoder(w).Encode(api.Switch{Id: &id})
	}))
	defer server.Close()

	output, err := executeCommand("switch", "reset", "1", "--url", server.URL, "--color=false")

	assert.NoError(t, err)
	assert.Contains(t, output, `"id": 1`)
}
