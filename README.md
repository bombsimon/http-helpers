# HTTP Helpers

Helping tools working with Go and HTTP.

## Middlewares

A collection of useful middlewares that may be re-use between different projects
involving an HTTP server. The `Middleware` type is just a function that takes a
`http.Handler` and returns a `http.Handler`. Each handler should (manually)
invoce the handlers `ServeHTTP()` method. The middlewares will be exdecuted in
the **reverse** order they're added, although depending on if you call
`ServeHTTP` before or after your code it might be executed in before or after
(or both of them) in relation to the actual handler. See
[tests](middleware/middleware_test.go) for more details.

```go
func main() {
    handlers := middleware.AddMiddlewares(
        mux.NewRouter(),
        MyMiddleware(),
    )

    http.ListenAndServe(":4080", handlers)
}

func MyMiddleware() http.Middleware {
    return func(h http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            fmt.Println("exdecuted before next middlewares/handlers")

            h.ServeHTTP(r, w)

            fmt.Println("executed after next middlewares/handlers")
        })
    }
}
```

### Logger

A logger used to log information about the HTTP request. The logging method
takes a
[`logrus.FieldLogger`](https://godoc.org/github.com/sirupsen/logrus#FieldLogger)
which enables advanced logging formats.

### PanicRecovery

A basic implementation of a panic recovery to ensure the server always stays
online.

## Server

Helpers working with HTTP servers.

### Graceful Shutdown

A graceful shutdown ensuring all connections to the HTTP server is drained
before shutting down. Just call this function to get a blocking channel that
will be closed when the server is drained.

```go
func main() {
    server := &http.Server{
        Addr: ":4080",
        Handler: mux.NewRouter(),
    }

    idleConnsClosed := GracefulShutdown(
        server,         // The HTTP server
        10*time.Second, // Wait time
        logrus.New(),   // Optional logger
    )

    if err := server.ListenAndServe(); err != nil {
        panic(err)
    }

    // Wait here until server is closed.
    <-idleConnsClosed
```
