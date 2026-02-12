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
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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
}

// processExpiredSwitch sends notifications for expired switches.
func (w *Worker) processExpiredSwitch(sw api.Switch) error {
	w.Logger.Info("Switch expired, sending final notifications", "id", *sw.Id)

	// Send External Notifiers (Shoutrrr)
	sendErr := w.sendNotifiers(sw)
	if sendErr != nil {
		w.Logger.Error("Failed to send notifications", "id", *sw.Id, "error", sendErr)

		// Set as failed if not already
		if sw.Status == nil || *sw.Status != api.SwitchStatusFailed {
			statusFailed := api.SwitchStatusFailed
			sw.Status = &statusFailed
			failureMsg := cases.Title(language.English).String(sendErr.Error())
			sw.FailureReason = &failureMsg

			_, err := w.Store.Update(*sw.Id, sw)
			if err != nil {
				return err
			}

			err = w.sendWebPush(sw, "Failed to trigger switch", failureMsg)
			if err != nil {
				return err
			}
		}
		return sendErr
	}

	// Send Web Push to the Owner (if subscribed)
	if sw.PushSubscription != nil {
		w.Logger.Debug("Switch expired, sending web push alert", "id", *sw.Id)
		err := w.sendWebPush(sw, "Switch Activated", "Your switch has triggered and notifications have been sent.")
		if err != nil {
			w.Logger.Error("Failed to send expiration web push", "id", *sw.Id, "error", err)
		}
	}

	if *sw.DeleteAfterTriggered {
		w.Logger.Debug("Auto-deleting switch after triggering", "id", *sw.Id)
		err := w.Store.Delete(*sw.Id)
		if err != nil {
			return err
		}
		return nil
	}

	w.Logger.Debug("Marking switch as triggered", "id", *sw.Id)
	statusTriggered := api.SwitchStatusTriggered
	sw.Status = &statusTriggered
	_, err := w.Store.Update(*sw.Id, sw)
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

	if sw.TriggerAt == nil {
		return errors.New("triggerAt should not be nil")
	}

	reminderDur, err := time.ParseDuration(*sw.ReminderThreshold)
	if err != nil {
		return err
	}

	now := time.Now().Unix()

	// TriggerAt is a Unix timestamp (integer)
	// reminderDur.Seconds() gives us the threshold in seconds
	thresholdTime := *sw.TriggerAt - int64(reminderDur.Seconds())

	w.Logger.Debug("Evaluating reminder eligibility",
		"id", *sw.Id,
		"threshold", time.Unix(thresholdTime, 0).Format(time.RFC3339),
		"TriggerAt", time.Unix(*sw.TriggerAt, 0).Format(time.RFC3339),
	)

	// If we are past the threshold AND the switch hasn't actually expired yet
	if now >= thresholdTime {
		w.Logger.Debug("Reminder threshold met, triggering web push", "id", *sw.Id)

		// Calculate time string for the message
		diff := *sw.TriggerAt - now
		remaining := time.Duration(diff) * time.Second
		remainingStr := remaining.Round(time.Second).String()

		title := "Expiring Soon"
		body := fmt.Sprintf("Your switch will trigger in %s. Time to check in.", remainingStr)

		err := w.sendWebPush(sw, title, body)
		if err != nil {
			return err
		}

		w.Logger.Debug("Marking reminder as sent in database", "id", *sw.Id)

		v := true
		sw.ReminderSent = &v

		_, reminderSentErr := w.Store.Update(*sw.Id, sw)
		return reminderSentErr
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

// sendWebPush sends a web push notification.
// Modified to accept title and body to support both Reminders and Expirations.
func (w *Worker) sendWebPush(sw api.Switch, title, body string) error {
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

	payload, err := json.Marshal(map[string]interface{}{
		"title": title,
		"body":  body,
		"data": map[string]interface{}{
			"id":  *sw.Id,
			"url": "/",
		},
	})
	if err != nil {
		return err
	}

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
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return errors.New("could not read web push body")
		}
		w.Logger.Debug("Web push not sent", "id", *sw.Id, "status_code", resp.StatusCode, "body", string(bodyBytes))
		return fmt.Errorf("could not send web push: %d", resp.StatusCode)
	}

	defer func() { _ = resp.Body.Close() }()

	w.Logger.Debug("Web push sent", "id", *sw.Id, "status_code", resp.StatusCode)

	return nil
}
