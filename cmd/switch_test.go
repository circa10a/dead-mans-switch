package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/circa10a/dead-mans-switch/api"
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
		if r.URL.Path != "/switch" {
			t.Errorf("expected path %q, got %q", "/switch", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected method %q, got %q", http.MethodPost, r.Method)
		}

		var body api.Switch
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Message != "test-message" {
			t.Errorf("expected message %q, got %q", "test-message", body.Message)
		}

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
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(output, `"id": 1`) {
		t.Errorf("expected output to contain %q, got %q", `"id": 1`, output)
	}
	if !strings.Contains(output, `"message": "test-message"`) {
		t.Errorf("expected output to contain %q, got %q", `"message": "test-message"`, output)
	}
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

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(output, `"message": "switch not found"`) {
		t.Errorf("expected output to contain %q, got %q", `"message": "switch not found"`, output)
	}
}

func Test_ResetCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/switch/1/reset" {
			t.Errorf("expected path %q, got %q", "/switch/1/reset", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		id := 1
		_ = json.NewEncoder(w).Encode(api.Switch{Id: &id})
	}))
	defer server.Close()

	output, err := executeCommand("switch", "reset", "1", "--url", server.URL, "--color=false")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(output, `"id": 1`) {
		t.Errorf("expected output to contain %q, got %q", `"id": 1`, output)
	}
}
func Test_DisableCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/switch/1/disable" {
			t.Errorf("expected path %q, got %q", "/switch/1/disable", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		id := 1
		status := api.SwitchStatusDisabled
		_ = json.NewEncoder(w).Encode(api.Switch{
			Id:     &id,
			Status: &status,
		})
	}))
	defer server.Close()

	output, err := executeCommand("switch", "disable", "1", "--url", server.URL, "--color=false")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(output, `"id": 1`) {
		t.Errorf("expected output to contain %q, got %q", `"id": 1`, output)
	}
	if !strings.Contains(output, `"status": "disabled"`) {
		t.Errorf("expected output to contain %q, got %q", `"status": "disabled"`, output)
	}
}
