package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/nicholas-fedor/shoutrrr"
)

// Worker periodically processes expired switches and sends notifications.
type Worker struct {
	Store     database.Store
	BatchSize int
	Interval  time.Duration
	Logger    *slog.Logger
}

// Start begins the worker's processing loop.
func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	w.Logger.Info("Starting notification worker", "interval", w.Interval.String())

	for {
		select {
		case <-ctx.Done():
			w.Logger.Info("Stopping notification worker")
			return
		case <-ticker.C:
			w.Logger.Debug("Checking for expired switches")
			w.sweep()
			w.Logger.Debug(fmt.Sprintf("Completed sweep for expired switches, next check at %s", time.Now().Add(w.Interval).Format(time.RFC3339)))
		}
	}
}

// Sweep processes expired switches in batches.
func (w *Worker) sweep() {
	expired, err := w.Store.GetExpired(w.BatchSize)
	if err != nil {
		w.Logger.Error("Failed to fetch expired switches", "error", err)
		return
	}

	for _, sw := range expired {
		w.Logger.Info("Processing expired switch", "id", *sw.Id)

		// Only delete from db if notifications were sent successfully
		err := w.sendNotifications(sw)
		if err != nil {
			w.Logger.Error("Skipping DB update due to notification failure", "id", *sw.Id, "error", err)
			continue // Skip Sent/Delete so we retry on the next sweep
		}

		if sw.DeleteAfterSent {
			if err := w.Store.Delete(*sw.Id); err != nil {
				w.Logger.Error("Failed to delete switch", "id", *sw.Id, "error", err)
			}
			continue
		}

		err = w.Store.Sent(*sw.Id)
		if err != nil {
			w.Logger.Error("Failed to mark switch as sent", "id", *sw.Id, "error", err)
		}
	}
}

// sendNotifications sends notifications for the given switch and returns an error if any delivery fails.
func (w *Worker) sendNotifications(sw api.Switch) error {
	for _, url := range sw.Notifiers {
		sender, err := shoutrrr.CreateSender(url)
		if err != nil {
			return fmt.Errorf("failed to create notifier for switch id %d: %w", *sw.Id, err)
		}

		errs := sender.Send(sw.Message, nil)
		for _, sendErr := range errs {
			if sendErr != nil {
				return fmt.Errorf("notification delivery failed for switch id %d: %w", *sw.Id, sendErr)
			}
		}

		w.Logger.Debug("Notification sent successfully", "id", *sw.Id)
	}

	return nil
}
