package server

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-playground/validator/v10"

	"github.com/go-chi/chi/v5"

	"github.com/caddyserver/certmagic"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/circa10a/dead-mans-switch/internal/server/handlers"
	"github.com/circa10a/dead-mans-switch/internal/server/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultLogLevel        = "info"
	defaultWorkerInterval  = 5 * time.Minute
	defaultWorkerBatchSize = 1000
)

//go:embed web/ui.html
var indexHTML []byte

//go:embed web/manifest.json
var manifestJSON []byte

//go:embed api.html
var apiDocs []byte

// Server is our web server that runs the network mirror.
type Server struct {
	Config

	ctx         context.Context
	cancel      context.CancelFunc
	mux         http.Handler
	logger      *slog.Logger
	middlewares []func(http.Handler) http.Handler
	Worker      *Worker
}

// Config holds configuration for creating a Server.
type Config struct {
	AutoTLS           bool
	Domains           []string
	EncryptionEnabled bool
	LogFormat         string
	LogLevel          string
	Metrics           bool
	Port              int
	StorageDir        string
	TLSCert           string
	TLSKey            string
	Validation        bool
	WorkerBatchSize   int
	WorkerInterval    time.Duration
}

// New returns a new server configured from cfg.
func New(cfg *Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		Config: *cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	if server.LogLevel == "" {
		server.LogLevel = defaultLogLevel
	}

	if server.WorkerBatchSize == 0 {
		server.WorkerBatchSize = defaultWorkerBatchSize
	}

	if server.WorkerInterval == 0 {
		server.WorkerInterval = defaultWorkerInterval
	}

	server.LogFormat = strings.ToLower(server.LogFormat)

	router := chi.NewRouter()
	server.mux = router

	// Ensure configuration options are valid/compatible
	err := server.validate()
	if err != nil {
		return nil, err
	}

	// Logging
	logLevel, err := log.ParseLevel(server.LogLevel)
	if err != nil {
		return nil, err
	}

	logHandler := log.NewWithOptions(os.Stdout, log.Options{
		ReportCaller:    true,
		ReportTimestamp: true,
		TimeFormat:      time.RFC3339,
		Formatter:       getLogFormatter(server.LogFormat),
		Level:           logLevel,
	})
	server.logger = slog.New(logHandler)

	// Database
	db, err := database.New(server.StorageDir, server.EncryptionEnabled)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	err = db.Init()
	if err != nil {
		return nil, fmt.Errorf("failed to create database tables: %w", err)
	}

	// Worker
	server.Worker = &Worker{
		Store:     db,
		Interval:  server.WorkerInterval,
		BatchSize: server.WorkerBatchSize,
		Logger:    server.logger,
	}
	go server.Worker.Start(server.ctx)

	// Features
	if server.Metrics {
		router.Handle("/metrics", promhttp.Handler())
		server.middlewares = append(server.middlewares, middleware.Prometheus)
	}

	// Default middlewares
	server.mux = middleware.Logging(server.logger, server.mux)

	// Add middlewares via http.Handler chaining
	for _, mw := range server.middlewares {
		server.mux = mw(server.mux)
	}

	// Routes
	// API docs
	router.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(apiDocs) })

	// Health check
	healthHandler := &handlers.Health{
		Store: db,
	}
	router.Get("/health", healthHandler.GetHandleFunc)

	// Switches
	switchHandler := &handlers.Switch{
		Validator: validator.New(),
		Store:     db,
		Logger:    server.logger,
	}

	// Apply the middleware only to where switches are created
	router.With(middleware.NotifierValidator).Post("/switch", switchHandler.PostHandleFunc)
	router.Get("/switch", switchHandler.GetHandleFunc)
	router.Get("/switch/{id}", switchHandler.GetByIDHandleFunc)
	router.Put("/switch/{id}", switchHandler.PutByIDHandleFunc)
	router.Delete("/switch/{id}", switchHandler.DeleteHandleFunc)
	router.Post("/switch/{id}/reset", switchHandler.ResetHandleFunc)

	// UI
	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexHTML)
	})
	// PWA manifest
	router.Get("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(manifestJSON)
	})

	return server, nil
}

// Start starts the listener of the server.
func (s *Server) Start() error {
	log := s.logger.With("component", "server")

	// Auto TLS will create listeners on port 80 and 443
	if s.AutoTLS {
		log.Info("Starting server on :80 and :443")
		certmagic.DefaultACME.Agreed = true
		certmagic.DefaultACME.Email = "user@oss.com"
		return certmagic.HTTPS(s.Domains, s.mux)
	}

	// If no auto TLS, use specified server port
	// :{port}
	addr := fmt.Sprintf(":%d", s.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       5 * time.Second,
	}

	log.Info("Starting server on " + addr)

	// If custom cert and key provided, listen on specified server port via https
	if s.TLSCert != "" && s.TLSKey != "" {
		return httpServer.ListenAndServeTLS(s.TLSCert, s.TLSKey)
	}

	// No TLS requirements specified, listen on specified server port via http
	return httpServer.ListenAndServe()
}

// Stop stops the server.
func (s *Server) Stop() {
	s.logger.Info("shutting down server")
	s.cancel()
}

// validate validates the server configuration and checks for conflicting parameters.
func (s *Server) validate() error {
	if !s.Validation {
		return nil
	}

	if s.AutoTLS && (s.TLSCert != "" || s.TLSKey != "") {
		return errors.New("AutoTLS cannot be set along with TLS cert or TLS key")
	}

	if s.AutoTLS && len(s.Domains) == 0 {
		return errors.New("AutoTLS requires a domain to also be configured")
	}

	if s.TLSCert != "" && s.TLSKey == "" {
		return errors.New("TLS certificate is missing TLS key")
	}

	if s.TLSCert == "" && s.TLSKey != "" {
		return errors.New("TLS key is missing TLS certificate")
	}

	validLogFormats := []string{"json", "text", ""}
	if !slices.Contains(validLogFormats, s.LogFormat) {
		return fmt.Errorf("invalid log format. Valid log formats are: %v", validLogFormats)
	}

	if s.LogLevel != "" {
		_, err := log.ParseLevel(s.LogLevel)
		if err != nil {
			return err
		}
	}

	return nil
}

// getLogFormatter converts a log format string to usable log formatter
func getLogFormatter(logformat string) log.Formatter {
	switch logformat {
	case "json":
		return log.JSONFormatter
	}
	return log.TextFormatter
}
