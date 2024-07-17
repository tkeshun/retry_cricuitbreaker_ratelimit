package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"golang.org/x/exp/slog"
)

var (
	rdb *redis.Client
)

func init() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     "redis:6379", // Redisサーバーのアドレス
		Password: "",           // パスワードなし
		DB:       0,            // デフォルトのDB
	})
}

func hostExists(url string) bool {
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func handler(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if host == "" {
		http.Error(w, "Host parameter is missing", http.StatusBadRequest)
		return
	}

	if hostExists(host) {
		count, err := rdb.Incr(context.Background(), "counter").Result()
		if err != nil {
			http.Error(w, "Could not increment counter", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "Counter: %d\n", count)
	} else {
		http.Error(w, "Host not found", http.StatusNotFound)
	}
}

func main() {
	addr := ":8080"
	handler := http.HandlerFunc(handler)
	server := &http.Server{Addr: addr, Handler: handler}
	idleConnsClosed := make(chan struct{})

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM) // SIGINT, SIGTERM を検知する
		<-c

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		slog.Info("Server is shutting down...")
		if err := server.Shutdown(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				slog.Warn("HTTP server Shutdown: timeout")
			} else {
				slog.Error("HTTP server Shutdown: ", err)
			}
			close(idleConnsClosed)
			return
		}
		slog.Info("Server is shut down")
		close(idleConnsClosed)
	}()

	slog.Info("Server is running on ", addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server ListenAndServe: ", err)
	}

	<-idleConnsClosed
}
