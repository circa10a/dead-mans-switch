package middleware

import (
	"net/http"

	metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	stdmiddleware "github.com/slok/go-http-metrics/middleware/std"
)

// Prometheus wraps an http.Handler to provide prometheus metrics for the route.
func Prometheus(next http.Handler) http.Handler {
	mw := middleware.New(middleware.Config{
		Recorder: metrics.NewRecorder(metrics.Config{}),
	})

	return stdmiddleware.Handler("", mw, next)
}
