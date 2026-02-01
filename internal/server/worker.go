package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/nicholas-fedor/shoutrrr"
)

// Worker periodically processes expired switches and sends notifications.
type Worker struct {
	Store           database.Store
	BatchSize       int
	Interval        time.Duration
	Logger          *slog.Logger
	SubscriberEmail string
	VAPIDPrivateKey string
	VAPIDPublicKey  string
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
	// Switches
	expired, err := w.Store.GetExpired(w.BatchSize)
	if err != nil {
		w.Logger.Error("Failed to fetch expired switches", "error", err)
		return
	}

	w.Logger.Debug("Fetched expired switches", "count", len(expired))

	for _, sw := range expired {
		err = w.processExpiredSwitch(sw)
		if err != nil {
			w.Logger.Error("Could not process expired switch", "error", err, "id", sw.Id)
		}
	}

	// Reminders
	reminders, err := w.Store.GetEligibleReminders(w.BatchSize)
	if err != nil {
		w.Logger.Error("Failed to fetch eligible reminders", "error", err)
		return
	}

	w.Logger.Debug("Fetched eligible reminders", "count", len(reminders))

	for _, sw := range reminders {
		err = w.processReminder(sw)
		if err != nil {
			w.Logger.Error("Could not send reminder", "error", err, "id", sw.Id)
		}
	}
}

// processExpiredSwitch sends notifications for expired switches.
func (w *Worker) processExpiredSwitch(sw api.Switch) error {
	w.Logger.Info("Switch expired, sending final notifications", "id", *sw.Id)

	err := w.sendNotifiers(sw)
	if err != nil {
		w.Logger.Error("Failed to send notifications", "id", *sw.Id, "error", err)
		return err
	}

	if sw.DeleteAfterSent {
		w.Logger.Debug("Auto-deleting switch after sending", "id", *sw.Id)
		err = w.Store.Delete(*sw.Id)
		if err != nil {
			return err
		}
		return nil
	}

	w.Logger.Debug("Marking switch as sent", "id", *sw.Id)
	err = w.Store.Sent(*sw.Id)
	if err != nil {
		return err
	}

	return nil
}

// processReminder sends reminders.
func (w *Worker) processReminder(sw api.Switch) error {
	if sw.ReminderThreshold == nil || *sw.ReminderThreshold == "" {
		return nil
	}

	reminderDur, err := time.ParseDuration(*sw.ReminderThreshold)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	threshold := sw.SendAt.Add(-reminderDur)

	w.Logger.Debug("Evaluating reminder eligibility",
		"id", *sw.Id,
		"now", now.Format(time.RFC3339),
		"threshold", threshold.Format(time.RFC3339),
	)

	// If we are past the threshold AND the switch hasn't actually expired yet
	if now.After(threshold) && now.Before(*sw.SendAt) {
		w.Logger.Info("Reminder threshold met, triggering web push", "id", *sw.Id)
		err := w.sendWebPush(sw)
		if err != nil {
			return err
		}

		w.Logger.Debug("Marking reminder as sent in database", "id", *sw.Id)
		return w.Store.ReminderSent(*sw.Id)
	}

	return nil
}

// sendNotifiers triggers configured notifiers
func (w *Worker) sendNotifiers(sw api.Switch) error {
	var errs []error

	for _, url := range sw.Notifiers {
		sender, err := shoutrrr.CreateSender(url)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to create sender for %s: %w", url, err))
			continue
		}

		sendErrs := sender.Send(sw.Message, nil)
		for _, sendErr := range sendErrs {
			if sendErr != nil {
				errs = append(errs, fmt.Errorf("delivery failed for %s: %w", url, sendErr))
			}
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// sendWebPush sends a web push notification for a reminder.
func (w *Worker) sendWebPush(sw api.Switch) error {
	if sw.PushSubscription == nil || sw.PushSubscription.Endpoint == nil {
		w.Logger.Debug("Skipping web push: no subscription found", "id", *sw.Id)
		return nil
	}

	w.Logger.Debug("Preparing web push payload", "id", *sw.Id, "endpoint", *sw.PushSubscription.Endpoint)

	s := &webpush.Subscription{
		Endpoint: *sw.PushSubscription.Endpoint,
	}

	if sw.PushSubscription.Keys != nil {
		if sw.PushSubscription.Keys.Auth != nil {
			s.Keys.Auth = *sw.PushSubscription.Keys.Auth
		}
		if sw.PushSubscription.Keys.P256dh != nil {
			s.Keys.P256dh = *sw.PushSubscription.Keys.P256dh
		}
	}

	now := time.Now().UTC()
	remaining := sw.SendAt.Sub(now)
	remainingStr := remaining.Round(time.Second).String()

	payload, _ := json.Marshal(map[string]interface{}{
		"title": "Switch Expiring Soon",
		"body":  fmt.Sprintf("Your switch will trigger in %s. Time to check in.", remainingStr),
		"data": map[string]interface{}{
			"id":  *sw.Id,
			"url": "/",
		},
	})

	resp, err := webpush.SendNotification(payload, s, &webpush.Options{
		VAPIDPublicKey:  w.VAPIDPublicKey,
		VAPIDPrivateKey: w.VAPIDPrivateKey,
		Subscriber:      w.SubscriberEmail,
		TTL:             3600,
		Urgency:         webpush.UrgencyHigh,
	})
	if err != nil {
		return err
	}

	if resp.StatusCode >= 399 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errors.New("could not read web push body")
		}
		w.Logger.Debug("Web push not sent", "id", *sw.Id, "status_code", resp.StatusCode, "body", string(body))
		return fmt.Errorf("could not send web push: %d", resp.StatusCode)
	}

	defer func() { _ = resp.Body.Close() }()

	w.Logger.Debug("Web push sent", "id", *sw.Id, "status_code", resp.StatusCode)

	return nil
}
