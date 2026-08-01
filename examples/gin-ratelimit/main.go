// Demo: wire valve httpmw into Gin without adding Gin to the core module.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	httpmw "github.com/shiv-eshwar/valve/pkg/middleware/http"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func main() {
	lim := limiter.New(memory.New())
	defer func() { _ = lim.Close(context.Background()) }()

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(valveGin(httpmw.Config{
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

	r.GET("/hello", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"ok":             true,
			"reservation_id": httpmw.ReservationIDFromContext(c.Request.Context()),
		})
	})

	addr := env("LISTEN", ":8088")
	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		log.Printf("gin-ratelimit listening on %s (header X-Subject)", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	_ = srv.Shutdown(context.Background())
}

// valveGin adapts httpmw.Middleware to gin.HandlerFunc.
func valveGin(cfg httpmw.Config) gin.HandlerFunc {
	mw := httpmw.Middleware(cfg)
	return func(c *gin.Context) {
		var nextCalled bool
		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			c.Request = r
			c.Next()
		})).ServeHTTP(c.Writer, c.Request)
		if !nextCalled {
			c.Abort()
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
