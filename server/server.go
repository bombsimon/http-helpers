package server

/*
Support graceful shutdown for your HTTP server by passing the server to this
function. Perofrming a signal interruption will drain the connections and close
the server. Example usage:

	func main() {
		server := &http.Server{
			Addr: ":4080",
			Handler: mux.NewRouter(),
		}

		idleConnsClosed := server.GracefulShutdown(
			server,         // The HTTP server
			10*time.Second, // Wait time
			logrus.New(),   // Optional logger
		)

		if err := server.ListenAndServe(); err != nil {
			panic(err)
		}

		<-idleConnsClosed
	}
*/

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ShutdownLogger implements logging for shutdown process.
type ShutdownLogger interface {
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// GracefulShutdown will enable graceful shutdown on the passed server.
func GracefulShutdown(server *http.Server, waitTime time.Duration, logger ShutdownLogger) chan struct{} {
	// Channel used to wait for draining. This channel will be returned and
	// should be used to block during shutdown.
	idleConnsClosed := make(chan struct{})

	go func() {
		gracefulStop := make(chan os.Signal, 1)

		signal.Notify(gracefulStop, syscall.SIGTERM)
		signal.Notify(gracefulStop, syscall.SIGINT)

		<-gracefulStop

		if logger != nil {
			logger.Infof("shutting down server, draining connections")
		}

		// Create a context with a timeout so we never wait longer than 10
		// seconds.
		ctx, cancelFunc := context.WithTimeout(context.Background(), waitTime)
		defer cancelFunc()

		if err := server.Shutdown(ctx); err != nil {
			if logger != nil {
				logger.Errorf("could not shut down server gracefully: %s", err)
			}
		}

		close(idleConnsClosed)
	}()

	return idleConnsClosed
}
