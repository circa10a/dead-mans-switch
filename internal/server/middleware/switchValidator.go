package middleware

import (
	"bytes"
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

// SwitchContextKey is the context key for accessing the value of a validated switch in handlers.
const SwitchContextKey contextKey = "validatedSwitch"

// FromContext grabs a Switch payload from the context to ensure we only read the body once
// since we read the body to perform validation in this middleware.
func FromContext(ctx context.Context) (api.Switch, bool) {
	sw, ok := ctx.Value(SwitchContextKey).(api.Switch)
	return sw, ok
}

// SwitchValidator handles JSON decoding and struct validation
func SwitchValidator(v *validator.Validate) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload := api.Switch{}

			// Read the body
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				sendJSONError(w, http.StatusBadRequest, "Read error")
				return
			}

			// Restore body for any subsequent reads (though usually not needed if using context)
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			err = json.Unmarshal(bodyBytes, &payload)
			if err != nil {
				sendJSONError(w, http.StatusBadRequest, "Invalid JSON")
				return
			}

			_, err = time.ParseDuration(payload.CheckInInterval)
			if err != nil {
				sendJSONError(w, http.StatusBadRequest, "Invalid checkInInterval time format. Examples are 10s, 10m, 10h, 10d")
				return
			}

			if payload.ReminderThreshold != nil && *payload.ReminderThreshold != "" {
				_, err = time.ParseDuration(*payload.ReminderThreshold)
				if err != nil {
					sendJSONError(w, http.StatusBadRequest, "Invalid ReminderThreshold format (e.g., 15m, 1h)")
					return
				}
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

			ctx := context.WithValue(r.Context(), SwitchContextKey, payload)
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
