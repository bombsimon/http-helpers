package middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
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
		t.Fatal("panich andler never called!")
	}

	if !strings.Contains(buf.String(), "i'm just going to panic here if that's ok...") {
		t.Fatal("did not log after panicing")
	}

}
