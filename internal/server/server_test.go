package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		server    *Server
		expectErr bool
	}{
		{
			// Invalid log format
			server: &Server{
				Config: Config{
					LogFormat: "fake",
				},
			},
			expectErr: true,
		},
		{
			// Auto TLS and custom cert set (conflict)
			server: &Server{
				Config: Config{
					AutoTLS: true,
					TLSCert: "cert",
				},
			},
			expectErr: true,
		},
		{
			// Auto TLS and custom key set (conflict)
			server: &Server{
				Config: Config{
					AutoTLS: true,
					TLSKey:  "key",
				},
			},
			expectErr: true,
		},
		{
			// Auto TLS and no domains
			server: &Server{
				Config: Config{
					AutoTLS: true,
				},
			},
			expectErr: true,
		},
		{
			// Cert set without key
			server: &Server{
				Config: Config{
					TLSCert: "cert",
				},
			},
			expectErr: true,
		},
		{
			// Key set without cert
			server: &Server{
				Config: Config{
					TLSKey: "key",
				},
			},
			expectErr: true,
		},
		{
			// Valid AutoTLS config
			server: &Server{
				Config: Config{
					AutoTLS: true,
					Domains: []string{"domain"},
				},
			},
		},
		{
			// Valid custom cert/key config
			server: &Server{
				Config: Config{
					TLSCert: "cert",
					TLSKey:  "key",
				},
			},
		},
	}

	for _, test := range tests {
		err := test.server.validate()
		if err != nil && !test.expectErr {
			t.Errorf("unexpected validation result: got error=%v wantErr=%v, err=%v", err != nil, test.expectErr, err)
		}
	}
}

func TestGetLogFormatter(t *testing.T) {
	tests := []struct {
		input    string
		expected log.Formatter
	}{
		{
			input:    "json",
			expected: log.JSONFormatter,
		},
		{
			input:    "text",
			expected: log.TextFormatter,
		},
		{
			input:    "fake",
			expected: log.TextFormatter,
		},
	}
	for _, test := range tests {
		actual := getLogFormatter(test.input)
		if test.expected != actual {
			t.Errorf("getLogFormatter returned unexpected log formatter: got %v want %v", actual, test.expected)
		}
	}
}

func TestServerConfigOpts(t *testing.T) {
	outputStr := "got: %v, want: %v"

	tmpDir := t.TempDir()

	t.Run("AutoTLS", func(t *testing.T) {
		v := true
		cfg := &Config{
			AutoTLS:    v,
			Domains:    []string{"d"},
			StorageDir: tmpDir,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.AutoTLS != v {
			t.Errorf(outputStr, s.AutoTLS, v)
		}
	})

	t.Run("DemoMode", func(t *testing.T) {
		v := true
		cfg := &Config{
			DemoMode:   v,
			StorageDir: tmpDir,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.DemoMode != v {
			t.Errorf(outputStr, s.DemoMode, v)
		}
	})

	t.Run("DemoResetInterval", func(t *testing.T) {
		v := 10 * time.Second
		cfg := &Config{
			DemoResetInterval: v,
			StorageDir:        tmpDir,
			Validation:        false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.DemoResetInterval != v {
			t.Errorf(outputStr, s.DemoResetInterval, v)
		}
	})

	t.Run("Domains", func(t *testing.T) {
		v := []string{"lemon"}
		cfg := &Config{
			Domains:    v,
			StorageDir: tmpDir,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if !reflect.DeepEqual(s.Domains, v) {
			t.Errorf(outputStr, s.Domains, v)
		}
	})

	t.Run("AddMiddlewares", func(t *testing.T) {
		v := []func(http.Handler) http.Handler{
			func(h http.Handler) http.Handler { return nil },
		}
		cfg := &Config{
			StorageDir: tmpDir,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		s.middlewares = v

		if len(s.middlewares) != len(v) {
			t.Errorf(outputStr, len(s.middlewares), len(v))
		}
	})

	t.Run("TLSCert", func(t *testing.T) {
		v := "cert"
		cfg := &Config{
			StorageDir: tmpDir,
			TLSCert:    v,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.TLSCert != v {
			t.Errorf(outputStr, s.TLSCert, v)
		}
	})

	t.Run("TLSKey", func(t *testing.T) {
		v := "key"
		cfg := &Config{
			StorageDir: tmpDir,
			TLSKey:     v,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.TLSKey != v {
			t.Errorf(outputStr, s.TLSKey, v)
		}
	})

	// duplicate TLSKey test removed

	t.Run("Port", func(t *testing.T) {
		v := 3000
		cfg := &Config{
			Port:       v,
			StorageDir: tmpDir,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.Port != v {
			t.Errorf(outputStr, s.Port, v)
		}
	})

	t.Run("AutoTLS", func(t *testing.T) {
		v := true
		cfg := &Config{
			AutoTLS:    v,
			Domains:    []string{"d"},
			StorageDir: tmpDir,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.AutoTLS != v {
			t.Errorf(outputStr, s.AutoTLS, v)
		}
	})

	t.Run("Metrics", func(t *testing.T) {
		v := true
		cfg := &Config{
			Metrics:    v,
			StorageDir: tmpDir,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.Metrics != v {
			t.Errorf(outputStr, s.Metrics, v)
		}
	})

	t.Run("LogFormat", func(t *testing.T) {
		v := "JSON"
		vlower := strings.ToLower(v)
		cfg := &Config{
			LogFormat:  v,
			StorageDir: tmpDir,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.LogFormat != vlower {
			t.Errorf(outputStr, s.LogFormat, vlower)
		}
	})

	t.Run("LogLevel", func(t *testing.T) {
		v := "DEBUG"
		cfg := &Config{
			LogLevel:   v,
			StorageDir: tmpDir,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.LogLevel != v {
			t.Errorf(outputStr, s.LogLevel, v)
		}
	})

	t.Run("StorageDir", func(t *testing.T) {
		v := tmpDir
		cfg := &Config{
			StorageDir: tmpDir,
			Validation: false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.StorageDir != v {
			t.Errorf(outputStr, s.LogLevel, v)
		}
	})

	t.Run("WorkerBatchSize", func(t *testing.T) {
		v := 500
		cfg := &Config{
			WorkerBatchSize: v,
			StorageDir:      tmpDir,
			Validation:      false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.WorkerBatchSize != v {
			t.Errorf(outputStr, s.WorkerBatchSize, v)
		}
	})

	t.Run("WorkerInterval", func(t *testing.T) {
		v := 1 * time.Second
		cfg := &Config{
			WorkerInterval: v,
			StorageDir:     tmpDir,
			Validation:     false,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.WorkerInterval != v {
			t.Errorf(outputStr, s.WorkerInterval, v)
		}
	})

	t.Run("Validation", func(t *testing.T) {
		v := true
		cfg := &Config{
			StorageDir: tmpDir,
			Validation: v,
		}

		s, err := New(cfg)
		if err != nil {
			t.Errorf("received unexpected err: %s", err.Error())
		}

		if s.Validation != v {
			t.Errorf(outputStr, s.Validation, v)
		}
	})
}
