package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/nicholas-fedor/shoutrrr/pkg/router"
)

// NotifierValidator validates the Shoutrrr URL in the notifier field for POST requests
func NotifierValidator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		// Put it back so it can be read again later by the handler
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Unmarshal just enough to get the notifier field
		payload := api.Switch{}
		err = json.Unmarshal(bodyBytes, &payload)
		if err != nil {
			// If JSON is invalid, let the Handler handle the error response
			next.ServeHTTP(w, r)
			return
		}

		// Validate with Shoutrrr
		serviceRouter := router.ServiceRouter{}
		for _, url := range payload.Notifiers {
			_, err = serviceRouter.Locate(url)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(api.Error{
					Code:    http.StatusBadRequest,
					Message: "Invalid notifier URL: " + err.Error(),
				})
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
