package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/go-playground/validator/v10"
)

// contextKey is a private type to avoid collisions in context
type contextKey string

// SwitchContextKey is the context key for accessing the value of a validated switch in
const (
	SwitchContextKey contextKey = "validatedSwitch"
)

// ValidatedSwitch contains parsed payload/time fields to prevent parsing twice.
type ValidatedSwitch struct {
	Payload                   api.Switch
	CheckInIntervalDuration   time.Duration
	ReminderThresholdDuration *time.Duration // Pointer since reminder is optional
}

// FromContext grabs a Switch payload from the context to ensure we only read the body once
// since we read the body to perform validation in this middleware.
func FromContext(ctx context.Context) (ValidatedSwitch, bool) {
	sw, ok := ctx.Value(SwitchContextKey).(ValidatedSwitch)
	return sw, ok
}

// SwitchValidator handles JSON decoding and struct validation
func SwitchValidator(v *validator.Validate) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload := api.Switch{}

			// Read the body
			// we don't need to restore the body since we pass the validated payload
			// through the context
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				sendJSONError(w, http.StatusBadRequest, "Read error")
				return
			}

			err = json.Unmarshal(bodyBytes, &payload)
			if err != nil {
				sendJSONError(w, http.StatusBadRequest, "Invalid JSON")
				return
			}

			checkInIntervalDuration, err := time.ParseDuration(payload.CheckInInterval)
			if err != nil {
				sendJSONError(w, http.StatusBadRequest, "Invalid checkInInterval time format. Examples are 10s, 10m, 10h, 10d")
				return
			}

			var reminderThresholdDuration *time.Duration
			if payload.ReminderThreshold != nil && *payload.ReminderThreshold != "" {
				d, err := time.ParseDuration(*payload.ReminderThreshold)
				if err != nil {
					sendJSONError(w, http.StatusBadRequest, "Invalid ReminderThreshold format (e.g., 15m, 1h)")
					return
				}
				reminderThresholdDuration = &d
			}

			err = v.Struct(payload)
			if err != nil {
				errMsgs := []string{}

				if ve, ok := err.(validator.ValidationErrors); ok {
					for _, fe := range ve {
						// fe.Field() is the struct field name
						// fe.Tag() is the failed constraint (e.g., "required")
						msg := fmt.Sprintf("field '%s' failed on validation: %s", fe.Field(), fe.Tag())
						errMsgs = append(errMsgs, msg)
					}
				}

				// Join messages or send the first one
				fullErrMsg := "Validation failed: " + strings.Join(errMsgs, ", ")
				sendJSONError(w, http.StatusBadRequest, "Validation failed: "+fullErrMsg)
				return
			}

			validatedData := ValidatedSwitch{
				Payload:                   payload,
				CheckInIntervalDuration:   checkInIntervalDuration,
				ReminderThresholdDuration: reminderThresholdDuration,
			}

			ctx := context.WithValue(r.Context(), SwitchContextKey, validatedData)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// sendError handles both the JSON response and logging of internal errors
func sendJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    code,
		"message": msg,
	})
}
