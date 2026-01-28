package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/circa10a/dead-mans-switch/api"
)

func TestNotifierValidator(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := api.Switch{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("next handler failed to read body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	})

	handlerToTest := NotifierValidator(nextHandler)

	tests := []struct {
		name           string
		method         string
		payload        api.Switch
		expectedStatus int
	}{
		{
			name:   "valid single logger url",
			method: http.MethodPost,
			payload: api.Switch{
				Notifiers: []string{"logger://"},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "valid multiple urls",
			method: http.MethodPost,
			payload: api.Switch{
				Notifiers: []string{"logger://", "discord://token@id"},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "invalid scheme in list",
			method: http.MethodPost,
			payload: api.Switch{
				Notifiers: []string{"logger://", "myscheme://bad-url"},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "empty notifier list",
			method: http.MethodPost,
			payload: api.Switch{
				Notifiers: []string{},
			},
			// Shoutrrr doesn't validate empty lists, but your
			// 'min=1' struct validation will catch this later.
			// However, if the middleware loops, it will just pass through.
			expectedStatus: http.StatusOK,
		},
		{
			name:   "malformed url in list",
			method: http.MethodPost,
			payload: api.Switch{
				Notifiers: []string{"not-a-url"},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "bypass for non-post requests",
			method: http.MethodGet,
			payload: api.Switch{
				Notifiers: []string{"garbage-url"},
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest(tt.method, "/switch", bytes.NewBuffer(body))
			rec := httptest.NewRecorder()

			handlerToTest.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d. Body: %s", tt.name, tt.expectedStatus, rec.Code, rec.Body.String())
			}
		})
	}
}
