package middleware

/*
A way to chain middlewares to create a HTTP handler and use multiple
middlewares. Example usage with gorilla/mux:

	func main() {
		router := mux.NewRouter()
		logger := logrus.New()

		handers := middleware.AddMiddlewares(
			router,
			middleware.PanicRecovery(logger),
			middleware.Logger(logger),
		)

		if err := http.ListenAndServe(":4080", handlers); err != nil {
			logger.WithError(err).Error("could not start server...")
		}
	}
*/

import (
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// Middleware represents a middleware function which will add a handler before
// the final http serve handler.
type Middleware func(http.Handler) http.Handler

// AddMiddlewares will add all middlewares in the passed orter and return a
// handler which may be used for the http server. Since they're added in the
// order they're passed, they will be executed in the reverse order.
func AddMiddlewares(h http.Handler, middlewares ...Middleware) http.Handler {
	for _, middleware := range middlewares {
		h = middleware(h)
	}

	return h
}

// Logger creates a logger in a http.Handler for the HTTP server.
func Logger(logger logrus.FieldLogger) Middleware {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()

			h.ServeHTTP(w, r)

			logger.WithFields(logrus.Fields{
				"method":         r.Method,
				"remote_address": r.RemoteAddr,
				"path":           r.URL.String(),
				"protocol":       r.Proto,
				"content_length": r.ContentLength,
				"elapsed":        fmt.Sprintf("%.3f %s", time.Since(startTime).Seconds()*1000, "ms"),
			}).Infof("request processed")
		})
	}
}

// PanicRecovery ensures that panics are handled.
func PanicRecovery(logger logrus.FieldLogger) Middleware {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("panic recovered: %s", r)
				}
			}()

			h.ServeHTTP(w, r)
		})
	}
}
