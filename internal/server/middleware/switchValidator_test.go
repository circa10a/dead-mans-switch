package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
)

func TestSwitchValidator(t *testing.T) {
	v := validator.New()
	mw := SwitchValidator(v)

	// Handler that uses the FromContext helper to verify success
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw, ok := FromContext(r.Context())
		if !ok {
			t.Error("Failed to retrieve switch from context using FromContext")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		assert.Equal(t, "test message", sw.Message)
		w.WriteHeader(http.StatusOK)
	})

	handlerToTest := mw(nextHandler)

	tests := []struct {
		name           string
		payload        interface{}
		expectedStatus int
	}{
		{
			name: "Success - Valid Payload",
			payload: map[string]interface{}{
				"message":         "test message",
				"checkInInterval": "24h",
				"notifiers":       []string{"discord://token"},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "Failure - Invalid CheckInInterval duration String",
			payload: map[string]interface{}{
				"message":         "test message",
				"checkInInterval": "99forever", // Will fail time.ParseDuration
				"notifiers":       []string{"discord://token"},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure - Invalid ReminderThreshold duration String",
			payload: map[string]interface{}{
				"message":           "test message",
				"reminderThreshold": ptr("99forever"), // Will fail time.ParseDuration
				"notifiers":         []string{"discord://token"},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure - Validation error (missing message)",
			payload: map[string]interface{}{
				"checkInInterval": "1h",
				"notifiers":       []string{"discord://token"},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure - Malformed JSON",
			payload:        `{"message": "incomplete"...`,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if s, ok := tt.payload.(string); ok {
				body = []byte(s)
			} else {
				body, _ = json.Marshal(tt.payload)
			}

			req := httptest.NewRequest("POST", "/switch", bytes.NewBuffer(body))
			rr := httptest.NewRecorder()

			handlerToTest.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
		})
	}
}

func TestFromContext_Empty(t *testing.T) {
	// Test that FromContext returns false when the key isn't present
	req := httptest.NewRequest("GET", "/", nil)
	_, ok := FromContext(req.Context())
	assert.False(t, ok, "FromContext should return false for empty context")
}

func ptr[T any](v T) *T {
	return &v
}
