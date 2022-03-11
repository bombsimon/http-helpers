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
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// ResponseWriterWithInfo is a response writer that can hold additional
// information which can help enrich code executed as middlewares.
type ResponseWriterWithInfo struct {
	http.ResponseWriter
	statusCode    int
	responseError error
}

// NewResponseWriter will convert the response writer to a
// *ResponseWriterWithInfo if it is one, otherwise wrap the response writer in
// such type.
func NewResponseWriter(r http.ResponseWriter) *ResponseWriterWithInfo {
	if rw, ok := r.(*ResponseWriterWithInfo); ok {
		return rw
	}

	return &ResponseWriterWithInfo{
		ResponseWriter: r,
		statusCode:     http.StatusOK,
	}
}

// WriteHeader will write the header to the response witer and store the status
// that was written.
func (r *ResponseWriterWithInfo) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// WriteError will store the error on the response writer.
func (r *ResponseWriterWithInfo) WriteError(err error) {
	r.responseError = err
}

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
			rw := NewResponseWriter(w)
			startTime := time.Now()

			h.ServeHTTP(rw, r)

			log := logger.WithFields(logrus.Fields{
				"method":         r.Method,
				"remote_address": r.RemoteAddr,
				"path":           r.URL.String(),
				"protocol":       r.Proto,
				"content_length": r.ContentLength,
				"status":         rw.statusCode,
				"elapsed":        fmt.Sprintf("%.3f %s", time.Since(startTime).Seconds()*1000, "ms"),
			})

			if rw.responseError != nil {
				log.WithError(rw.responseError).Error("request processed")
			} else {
				log.Infof("request processed")
			}
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

//nolint:gochecknoglobals // Since promauto automatically registers metrics we
//want to ensure they're only registered once to not panic.
var metricsRegisterOnce = sync.Once{}

// Prometheus will add metrics for the request to prometheus.
func Prometheus() Middleware {
	var (
		inFlightGauge prometheus.Gauge
		counter       *prometheus.CounterVec
		duration      *prometheus.HistogramVec
		responseSize  *prometheus.HistogramVec
	)

	// Only register the collectors once even if the middleware is used multiple
	// times in the same process.
	metricsRegisterOnce.Do(func() {
		inFlightGauge = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "in_flight_requests",
			Help: "A gauge of requests currently being served by the handler.",
		})

		counter = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "A counter for requests to the handler.",
			},
			[]string{"code", "method"},
		)

		duration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "request_duration_seconds",
				Help:    "A histogram of latencies for requests.",
				Buckets: []float64{.01, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"method"},
		)

		responseSize = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "response_size_bytes",
				Help:    "A histogram of response sizes for requests.",
				Buckets: []float64{200, 500, 900, 1500},
			},
			[]string{},
		)
	})

	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ensure we copy the handler so we don't wrap the same handler for
			// each handler.
			handler := h

			handler = promhttp.InstrumentHandlerResponseSize(responseSize, handler)
			handler = promhttp.InstrumentHandlerInFlight(inFlightGauge, handler)
			handler = promhttp.InstrumentHandlerDuration(
				duration.MustCurryWith(prometheus.Labels{"method": r.Method}),
				handler,
			)

			rw := NewResponseWriter(w)

			handler.ServeHTTP(rw, r)

			counter.WithLabelValues(strconv.Itoa(rw.statusCode), r.Method).Inc()
		})
	}
}

// RateLimiter is a middleware that rate limits requests.
func RateLimiter(interval time.Duration, limit, burst int) Middleware {
	limiter := rate.NewLimiter(
		rate.Every(interval),
		limit,
	)

	limiter.SetBurst(burst)

	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				return
			}

			h.ServeHTTP(w, r)
		})
	}
}
