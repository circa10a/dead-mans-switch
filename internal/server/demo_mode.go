package server

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
)

const demoHealthCheckInterval = 5 * time.Minute

var demoSwitches = []struct {
	CheckInInterval string
	Message         string
	Notifiers       []string
}{

	{
		CheckInInterval: "1h",
		Message:         "Short Check-in (1 hour)",
		Notifiers:       []string{"logger://"},
	},
	{
		CheckInInterval: "24h",
		Message:         "Regular Check-in (1 hour)",
		Notifiers:       []string{"logger://"},
	},
	{
		CheckInInterval: "30s",
		Message:         "Failed notification",
		Notifiers:       []string{"generic://test"},
	},
}

// initDemoMode sets up demo switches and recreates them at the specified interval.
func (s *Server) initDemoMode(store database.Store) error {
	log := s.logger.With("component", "demo-mode")

	// Clear existing switches
	err := clearAllSwitches(store)
	if err != nil {
		return fmt.Errorf("failed to clear switches for demo mode: %w", err)
	}

	// Create initial demo switches
	err = createDemoSwitches(store)
	if err != nil {
		return fmt.Errorf("failed to create demo switches: %w", err)
	}

	log.Info("demo mode initialized with sample switches")

	// Start periodic reset goroutine
	if s.DemoResetInterval > 0 {
		go periodicDemoReset(s.ctx, s.logger, store, s.DemoResetInterval)
	}

	// Start periodic health check pinger for each domain
	if len(s.Domains) > 0 {
		go periodicHealthPing(s.ctx, s.logger, s.Domains)
	}

	return nil
}

// createDemoSwitches creates sample switches in the database
func createDemoSwitches(store database.Store) error {
	for _, demoData := range demoSwitches {
		duration, err := time.ParseDuration(demoData.CheckInInterval)
		if err != nil {
			return err
		}

		// Calculate trigger time
		triggerAt := time.Now().Add(duration).Unix()

		sw := api.Switch{
			CheckInInterval: demoData.CheckInInterval,
			Message:         demoData.Message,
			Notifiers:       demoData.Notifiers,
			Status:          ptrSwitchStatus(api.SwitchStatusActive),
			TriggerAt:       &triggerAt,
		}

		_, err = store.Create(sw)
		if err != nil {
			continue
		}
	}

	return nil
}

// clearAllSwitches removes all switches from the database
func clearAllSwitches(store database.Store) error {
	// Get all switches
	switches, err := store.GetAll(database.AdminUser, -1)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get all switches: %w", err)
	}

	// Delete each switch
	for _, sw := range switches {
		if sw.Id != nil {
			err := store.Delete(database.AdminUser, *sw.Id)
			if err != nil {
				return fmt.Errorf("failed to delete switch %d: %w", *sw.Id, err)
			}
		}
	}

	return nil
}

// periodicDemoReset periodically clears and recreates demo switches
func periodicDemoReset(ctx context.Context, logger *slog.Logger, store database.Store, resetInterval time.Duration) {
	log := logger.With("component", "demo-reset")
	ticker := time.NewTicker(resetInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("demo reset goroutine stopped")
			return
		case <-ticker.C:
			err := clearAllSwitches(store)
			if err != nil {
				log.Error("failed to clear switches during periodic reset", "error", err)
				continue
			}

			err = createDemoSwitches(store)
			if err != nil {
				log.Error("failed to recreate demo switches during periodic reset", "error", err)
				continue
			}

			log.Debug("periodic demo reset completed")
		}
	}
}

// Helper function for creating pointers to api types
func ptrSwitchStatus(s api.SwitchStatus) *api.SwitchStatus {
	return &s
}

// periodicHealthPing sends GET requests to https://<domain>every 5 minutes
// for each configured domain to keep the demo instance alive.
func periodicHealthPing(ctx context.Context, logger *slog.Logger, domains []string) {
	log := logger.With("component", "demo-ping")
	ticker := time.NewTicker(demoHealthCheckInterval)
	defer ticker.Stop()

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("health ping goroutine stopped")
			return
		case <-ticker.C:
			for _, domain := range domains {
				url := fmt.Sprintf("https://%s/", domain)

				req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
				if err != nil {
					log.Error("failed to create health ping request", "domain", domain, "error", err)
					continue
				}

				resp, err := client.Do(req)
				if err != nil {
					log.Error("health ping failed", "domain", domain, "error", err)
					continue
				}
				defer func() { _ = resp.Body.Close() }()

				log.Debug("health ping completed", "domain", domain, "status", resp.StatusCode)
			}
		}
	}
}
