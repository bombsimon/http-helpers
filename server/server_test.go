package server

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	serverURI = ":1337"
)

func Test_GracefulShutdown(t *testing.T) {
	var (
		timesCalled  int
		expctedCalls = 10
		wg           = &sync.WaitGroup{}
	)

	// Create a handler that will take a long time. This handler will be
	// registered on the http.DefaultServerMux.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("this will take a while...")

		// Tell the wait group that we've received the request.
		wg.Done()

		// Simulate long during process to enforce gracefulness.
		time.Sleep(1 * time.Second)
		fmt.Fprintf(w, "sorry for the delay...")
	})

	// Create a server with the DefaultServerMux.
	server := &http.Server{
		Addr:    serverURI,
		Handler: http.DefaultServeMux,
	}

	// Create our idle chan which will block until all connections are drained.
	idleChan := GracefulShutdown(server, 5*time.Second, logrus.New())

	// Start the server in a go routine and ensure there's no error.
	go func() {
		if err := server.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				t.Fatal(err)
			}
		}
	}()

	// TODO: I haven't found a way to tell when the server has actually started
	// so for now I'll just wait a bit before performing my requests.
	time.Sleep(500 * time.Millisecond)

	for i := 0; i < expctedCalls; i++ {
		wg.Add(1)

		go func() {
			result, err := http.Get("http://127.0.0.1:1337")
			if err != nil {
				t.Fatal("could not send http request")
			}

			defer result.Body.Close()

			b, err := ioutil.ReadAll(result.Body)
			if err != nil {
				t.Fatal("could not read response")
			}

			if string(b) != "sorry for the delay..." {
				t.Fatal("unexpected response")
			}

			timesCalled++
		}()
	}

	// Block until all requests have been received (but not necessarily
	// processed)
	wg.Wait()

	// Let's kill the process too see how graceful we are.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal("could not send SIGINT")
	}

	// Block until server is shut.
	<-idleChan

	// Ensure that all requests has been processed.
	if timesCalled != expctedCalls {
		t.Fatal("did not get response from all request")
	}
}
