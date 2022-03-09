package middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func Test_Logger(t *testing.T) {
	var (
		logger = logrus.New()
		buf    = &bytes.Buffer{}
	)

	logger.SetOutput(buf)
	logger.Formatter = &logrus.JSONFormatter{}

	handlerWithMiddleware := AddMiddlewares(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		Logger(logger),
	)

	ts := httptest.NewServer(handlerWithMiddleware)

	defer ts.Close()

	_, err := http.Post(ts.URL, "text/plain", bytes.NewReader([]byte("hello, world")))
	if err != nil {
		t.Fatal("could not send http request")
	}

	scanner := bufio.NewScanner(buf)
	logged := map[string]interface{}{}

	for scanner.Scan() {
		b := scanner.Bytes()

		if err := json.Unmarshal(b, &logged); err != nil {
			t.Fatal("could not parse logged message")
		}
	}

	for k, v := range map[string]interface{}{
		"method":         "POST",
		"msg":            "request processed",
		"level":          "info",
		"path":           "/",
		"protocol":       "HTTP/1.1",
		"content_length": float64(12),
	} {
		if logged[k] != v {
			t.Fatal("key mismatch:", k)
		}
	}
}

func Test_RateLimiter(t *testing.T) {
	requestsAllowedBeforeRateLimiting := 2
	expectedTimeBeforeRateLimiting := 10 * time.Millisecond

	handlerWithMiddleware := AddMiddlewares(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		RateLimiter(
			expectedTimeBeforeRateLimiting,
			requestsAllowedBeforeRateLimiting,
		),
	)

	ts := httptest.NewServer(handlerWithMiddleware)
	defer ts.Close()

	assertStatusCode := func(got, expected int) {
		if got != expected {
			t.Fatalf("unexpected status code, got: %v, expected: %v", got, expected)
		}
	}

	// Do as many requests as we're allowed + 1. On the last one we are
	// expected to be rate limited.
	for i := 0; i <= requestsAllowedBeforeRateLimiting; i++ {
		response, _ := http.Get(ts.URL)

		expectedStatus := http.StatusOK
		if i == requestsAllowedBeforeRateLimiting {
			expectedStatus = http.StatusTooManyRequests
		}

		assertStatusCode(response.StatusCode, expectedStatus)
	}
	// Sleeping in tests isn't great but I reckon this short time is ok...
	// Sorry!
	time.Sleep(expectedTimeBeforeRateLimiting)

	// We should now be able to request again.
	response, _ := http.Get(ts.URL)
	assertStatusCode(response.StatusCode, http.StatusOK)
}

func Test_PanicRecovery(t *testing.T) {
	var (
		logger         = logrus.New()
		buf            = &bytes.Buffer{}
		inPanicHandler = make(chan struct{})
	)

	logger.SetOutput(buf)

	handlerWithMiddleware := AddMiddlewares(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			go func() {
				inPanicHandler <- struct{}{}
			}()

			panic("i'm just going to panic here if that's ok...")
		}),
		Logger(logger),
		PanicRecovery(logger),
	)

	ts := httptest.NewServer(handlerWithMiddleware)

	defer ts.Close()

	_, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal("could not send http request")
	}

	select {
	case <-inPanicHandler:
		// We called the panic handler!
	case <-time.After(10 * time.Millisecond):
		t.Fatal("panic handler never called!")
	}

	if !strings.Contains(buf.String(), "i'm just going to panic here if that's ok...") {
		t.Fatal("did not log after panicing")
	}
}

func Test_Order(t *testing.T) {
	var (
		buf              = &bytes.Buffer{}
		logger           = log.New(buf, "", log.LstdFlags)
		createMiddleware = func(logString string) func(h http.Handler) http.Handler {
			return func(h http.Handler) http.Handler {
				return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
					// This will be called BEFORE the next middleware and
					// handler.
					logger.Println(logString)

					// This is the next middleware or handler.
					h.ServeHTTP(rw, r)

					// This will be called AFTER the next middleware or handler.
					logger.Println(logString)
				})
			}
		}
	)

	logger.SetOutput(buf)

	handlerWithMiddleware := AddMiddlewares(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			logger.Println("the handler")
		}),
		createMiddleware("one"),
		createMiddleware("two"),
		createMiddleware("three"),
	)

	ts := httptest.NewServer(handlerWithMiddleware)

	defer ts.Close()

	_, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal("could not send http request")
	}

	expectedOutput := []string{
		// These are the results by logging before h.ServeHTTP and will be in
		// the reverse order from how they're passed to AddMiddlewares().
		"three", "two", "one",
		// This simulates thehandler.
		"the handler",
		// These are the results by logging after h.ServeHTTP and will be in the
		// same order they're passed to AddMiddlewares().
		"one", "two", "three",
	}
	ordereredOutput := strings.Split(buf.String(), "\n")
	ordereredOutput = ordereredOutput[:len(ordereredOutput)-1] // Remove last empty string.

	if len(expectedOutput) != len(ordereredOutput) {
		t.Fatal("logs missmatched")
	}

	for i := range expectedOutput {
		if !strings.Contains(ordereredOutput[i], expectedOutput[i]) {
			t.Fatal("missmatched order or middleware")
		}
	}
}
