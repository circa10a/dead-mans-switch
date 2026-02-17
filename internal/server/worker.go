package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/circa10a/shoutrrr"
)

// worker periodically processes expired switches and sends notifications.
type worker struct {
	store           database.Store
	batchSize       int
	interval        time.Duration
	logger          *slog.Logger
	subscriberEmail string
	vapidPrivateKey string
	vapidPublicKey  string
}

// start begins the worker's processing loop.
func (w *worker) start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.logger.Info("Starting notification worker", "interval", w.interval.String())

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Stopping notification worker")
			return
		case <-ticker.C:
			w.logger.Debug("Checking for expired switches")
			w.sweep()
			w.logger.Debug(fmt.Sprintf("Completed sweep for expired switches, next check at %s", time.Now().Add(w.interval).Format(time.RFC3339)))
		}
	}
}

// Sweep processes expired switches in batches.
func (w *worker) sweep() {
	// Reminders
	reminders, err := w.store.GetEligibleReminders(w.batchSize)
	if err != nil {
		w.logger.Error("Failed to fetch eligible reminders", "error", err)
		return
	}

	w.logger.Debug("Fetched eligible reminders", "count", len(reminders))

	for _, sw := range reminders {
		err = w.processReminder(sw)
		if err != nil {
			w.logger.Error("Could not send reminder", "error", err, "id", sw.Id)
		}
	}

	// Switches
	expired, err := w.store.GetExpired(w.batchSize)
	if err != nil {
		w.logger.Error("Failed to fetch expired switches", "error", err)
		return
	}

	w.logger.Debug("Fetched expired switches", "count", len(expired))

	for _, sw := range expired {
		err = w.processExpiredSwitch(sw)
		if err != nil {
			w.logger.Error("Could not process expired switch", "error", err, "id", sw.Id)
		}
	}
}

// processExpiredSwitch sends notifications for expired switches.
func (w *worker) processExpiredSwitch(sw api.Switch) error {
	w.logger.Info("Switch expired, sending final notifications", "id", *sw.Id)

	// Send External Notifiers (Shoutrrr)
	sendErr := w.sendNotifiers(sw)
	if sendErr != nil {
		w.logger.Error("Failed to send notifications", "id", *sw.Id, "error", sendErr)

		// Set as failed if not already
		if sw.Status == nil || *sw.Status != api.SwitchStatusFailed {
			statusFailed := api.SwitchStatusFailed
			sw.Status = &statusFailed
			failureMsg := capitalizeFirst(sendErr.Error())
			sw.FailureReason = &failureMsg

			_, err := w.store.Update(*sw.Id, sw)
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
		w.logger.Debug("Switch expired, sending web push alert", "id", *sw.Id)
		err := w.sendWebPush(sw, "Switch Activated", "Your switch has triggered and notifications have been sent.")
		if err != nil {
			w.logger.Error("Failed to send expiration web push", "id", *sw.Id, "error", err)
		}
	}

	if *sw.DeleteAfterTriggered {
		w.logger.Debug("Auto-deleting switch after triggering", "id", *sw.Id)

		userID := database.AdminUser
		if sw.UserId != nil {
			userID = *sw.UserId
		}
		err := w.store.Delete(userID, *sw.Id)
		if err != nil {
			return err
		}
		return nil
	}

	w.logger.Debug("Marking switch as triggered", "id", *sw.Id)
	statusTriggered := api.SwitchStatusTriggered
	sw.Status = &statusTriggered
	_, err := w.store.Update(*sw.Id, sw)
	if err != nil {
		return err
	}

	return nil
}

// processReminder sends reminders.
func (w *worker) processReminder(sw api.Switch) error {
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

	w.logger.Debug("Evaluating reminder eligibility",
		"id", *sw.Id,
		"threshold", time.Unix(thresholdTime, 0).Format(time.RFC3339),
		"TriggerAt", time.Unix(*sw.TriggerAt, 0).Format(time.RFC3339),
	)

	// If we are past the threshold AND the switch hasn't actually expired yet
	if now >= thresholdTime {
		w.logger.Debug("Reminder threshold met, triggering web push", "id", *sw.Id)

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

		w.logger.Debug("Marking reminder as sent in database", "id", *sw.Id)

		v := true
		sw.ReminderSent = &v

		_, reminderSentErr := w.store.Update(*sw.Id, sw)
		return reminderSentErr
	}

	return nil
}

// sendNotifiers triggers configured notifiers
func (w *worker) sendNotifiers(sw api.Switch) error {
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
func (w *worker) sendWebPush(sw api.Switch, title, body string) error {
	if sw.PushSubscription == nil || sw.PushSubscription.Endpoint == nil {
		w.logger.Debug("Skipping web push: no subscription found", "id", *sw.Id)
		return nil
	}

	w.logger.Debug("Preparing web push payload", "id", *sw.Id, "endpoint", *sw.PushSubscription.Endpoint)

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
		VAPIDPublicKey:  w.vapidPublicKey,
		VAPIDPrivateKey: w.vapidPrivateKey,
		Subscriber:      w.subscriberEmail,
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
		w.logger.Debug("Web push not sent", "id", *sw.Id, "status_code", resp.StatusCode, "body", string(bodyBytes))
		return fmt.Errorf("could not send web push: %d", resp.StatusCode)
	}

	defer func() { _ = resp.Body.Close() }()

	w.logger.Debug("Web push sent", "id", *sw.Id, "status_code", resp.StatusCode)

	return nil
}

// capitalizeFirst uppercases the first character of a string.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
