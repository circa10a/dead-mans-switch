package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-playground/validator/v10"

	"github.com/go-chi/chi/v5"

	"github.com/caddyserver/certmagic"
	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/circa10a/dead-mans-switch/internal/server/handlers"
	"github.com/circa10a/dead-mans-switch/internal/server/middleware"
	"github.com/circa10a/dead-mans-switch/internal/server/secrets"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultLogLevel        = "info"
	defaultWorkerInterval  = 1 * time.Minute
	defaultWorkerBatchSize = 1000
)

//go:embed web/*
var webAssets embed.FS

//go:embed docs/api.internal.html
var internalAPIDocs []byte

//go:embed docs/api.public.html
var publicAPIDocs []byte

// Server is our web server that runs the network mirror.
type Server struct {
	Config

	ctx            context.Context
	cancel         context.CancelFunc
	mux            http.Handler
	logger         *slog.Logger
	middlewares    []func(http.Handler) http.Handler
	vapidPublicKey string
	worker         *worker
}

// Config holds configuration for creating a Server.
type Config struct {
	AuthEnabled       bool
	AuthIssuerURL     string
	AuthAudience      string
	AutoTLS           bool
	ContactEmail      string
	DemoMode          bool
	DemoResetInterval time.Duration
	Domains           []string
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
	db, err := database.NewSQLiteStore(server.StorageDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	err = db.Init()
	if err != nil {
		return nil, fmt.Errorf("failed to create database tables: %w", err)
	}

	// Create VAPID keys for push notifications
	vapidPrivPath := filepath.Join(server.StorageDir, "vapid.priv")
	vapidPubPath := filepath.Join(server.StorageDir, "vapid.pub")

	priv, pub, err := secrets.LoadOrCreateVAPIDKeys(vapidPrivPath, vapidPubPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize VAPID keys: %w", err)
	}

	// Server serves the key
	server.vapidPublicKey = pub

	// Demo mode
	if server.DemoMode {
		err = server.initDemoMode(db)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize demo mode: %w", err)
		}
	}

	// Worker
	server.worker = &worker{
		store:           db,
		interval:        server.WorkerInterval,
		batchSize:       server.WorkerBatchSize,
		logger:          server.logger,
		subscriberEmail: server.ContactEmail,
		// worker validates the sub claim
		vapidPublicKey: server.vapidPublicKey,
		// worker signs the push
		vapidPrivateKey: priv,
	}
	go server.worker.start(server.ctx)

	// Features
	if server.Metrics {
		router.Handle("/metrics", promhttp.Handler())
		server.middlewares = append(server.middlewares, middleware.Prometheus)
	}

	// JWT Authentication
	var jwtValidator *middleware.JWTValidator
	if server.AuthEnabled {
		var err error
		publicKeys, err := middleware.FetchPublicKeys(server.AuthIssuerURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch public keys from issuer: %w", err)
		}

		jwtValidator = &middleware.JWTValidator{
			Enabled:    true,
			IssuerURL:  server.AuthIssuerURL,
			Audience:   server.AuthAudience,
			PublicKeys: publicKeys,
		}
	} else {
		jwtValidator = &middleware.JWTValidator{
			Enabled: false,
		}
	}

	// Default middlewares
	server.mux = middleware.Logging(server.logger, server.mux)

	// Add middlewares via http.Handler chaining
	for _, mw := range server.middlewares {
		server.mux = mw(server.mux)
	}

	// Routes
	// Auth configuration endpoint (unauthenticated so UI can discover OIDC settings)
	authCfg := api.AuthConfig{
		Enabled: server.AuthEnabled,
	}
	if server.AuthEnabled {
		authCfg.Audience = &server.AuthAudience
		authCfg.IssuerUrl = &server.AuthIssuerURL
	}

	// Health check
	healthHandler := &handlers.Health{
		Store: db,
	}
	router.Get("/health", healthHandler.GetHandleFunc)

	// Switches
	switchHandler := &handlers.Switch{
		Store:  db,
		Logger: server.logger,
	}

	validator := validator.New()

	// API docs
	router.Route("/v1/docs", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(publicAPIDocs)
		})
		r.Get("/internal", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(internalAPIDocs)
		})
	})

	// Mount API v1 routes
	router.Route("/api/v1", func(r chi.Router) {
		// Unauthenticated routes
		r.Group(func(r chi.Router) {
			r.Get("/auth/config", handlers.AuthConfigHandler(authCfg))
		})

		// Apply JWT auth middleware to authenticated routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.JWTAuth(jwtValidator))

			// Group routes that require the validation middlewares
			r.Group(func(r chi.Router) {
				r.Use(middleware.SwitchValidator(validator))
				r.Use(middleware.NotifierValidator)

				r.Post("/switch", switchHandler.PostHandleFunc)
				r.Put("/switch/{id}", switchHandler.PutByIDHandleFunc)
			})

			// Standard routes (No body validation needed)
			r.Get("/switch", switchHandler.GetHandleFunc)
			r.Get("/switch/{id}", switchHandler.GetByIDHandleFunc)
			r.Delete("/switch/{id}", switchHandler.DeleteHandleFunc)
			r.Post("/switch/{id}/reset", switchHandler.ResetHandleFunc)
			r.Post("/switch/{id}/disable", switchHandler.DisableHandleFunc)

			// VAPID key for push notifications
			r.Get("/vapid", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(server.vapidPublicKey))
			})
		})
	})

	// UI
	publicFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		return server, err
	}
	fileServer := http.FileServer(http.FS(publicFS))

	router.Route("/", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			content, _ := webAssets.ReadFile("web/index.html")
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(content)
		})
		r.Handle("/*", fileServer)
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
		certmagic.DefaultACME.Email = s.ContactEmail
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
	s.logger.Info("Shutting down server")
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
