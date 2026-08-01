// Demo: wire valve httpmw into Echo without adding Echo to the core module.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/labstack/echo/v4"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	httpmw "github.com/shiv-eshwar/valve/pkg/middleware/http"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func main() {
	lim := limiter.New(memory.New())
	defer func() { _ = lim.Close(context.Background()) }()

	e := echo.New()
	e.HideBanner = true
	e.Use(valveEcho(httpmw.Config{
		Limiter: lim,
		Key: func(req *http.Request) api.Key {
			sub := req.Header.Get("X-Subject")
			if sub == "" {
				sub = "anonymous"
			}
			return api.Key{Subject: sub, Model: "demo"}
		},
		Limits: func(*http.Request) api.Limits {
			return api.Limits{RequestsPerMinute: 5, TokensPerMinute: 1000}
		},
		Cost: func(*http.Request) api.Cost {
			return api.Cost{Requests: 1, Tokens: 100}
		},
	}))

	e.GET("/hello", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"ok":             true,
			"reservation_id": httpmw.ReservationIDFromContext(c.Request().Context()),
		})
	})

	addr := env("LISTEN", ":8089")
	go func() {
		log.Printf("echo-ratelimit listening on %s (header X-Subject)", addr)
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	_ = e.Shutdown(context.Background())
}

func valveEcho(cfg httpmw.Config) echo.MiddlewareFunc {
	mw := httpmw.Middleware(cfg)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var nextErr error
			var nextCalled bool
			mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				c.SetRequest(r)
				nextErr = next(c)
			})).ServeHTTP(c.Response(), c.Request())
			if !nextCalled {
				return nil // deny already written
			}
			return nextErr
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
