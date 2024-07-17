package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/sony/gobreaker"
	"go.uber.org/ratelimit"
)

var (
	rdb         *redis.Client
	cb          *gobreaker.CircuitBreaker
	rateLimiter ratelimit.Limiter
	client      *retryablehttp.Client
)

func init() {
	// Redisサーバーのアドレスを設定
	rdb = redis.NewClient(&redis.Options{
		Addr:     "redis:6379", // Redisサーバーのアドレス
		Password: "",           // パスワードなし
		DB:       0,            // デフォルトのDB
	})

	cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:    "Redis",
		Timeout: 30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 3 // 3回連続失敗でトリップ
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			slog.Info("Circuit breaker state changed", "name", name, "from", from.String(), "to", to.String()) // サーキットブレイカーの状態が変わるたびにログ出力
		},
	})

	rateLimiter = ratelimit.New(1, ratelimit.Per(10*time.Second)) // 1リクエスト/秒

	client = retryablehttp.NewClient()
	client.RetryMax = 3 // 最大3回リトライ
}

func hostExists(url string) bool {
	req, err := retryablehttp.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func handler(w http.ResponseWriter, r *http.Request) {
	// レートリミットの適用
	rateLimiter.Take()

	host := r.URL.Query().Get("host")
	if host == "" {
		http.Error(w, "Host parameter is missing", http.StatusBadRequest)
		return
	}

	if hostExists(host) {
		// サーキットブレーカーの適用
		body, err := cb.Execute(func() (interface{}, error) {
			count, err := rdb.Incr(context.Background(), "counter").Result()
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("Counter: %d\n", count), nil
		})

		if err != nil {
			http.Error(w, "Could not increment counter", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, body.(string))
	} else {
		http.Error(w, "Host not found", http.StatusNotFound)
	}
}

func main() {
	addr := ":8080"
	handler := http.HandlerFunc(handler)
	server := &http.Server{Addr: addr, Handler: handler}
	idleConnsClosed := make(chan struct{})

	// シグナルを受け取るためのゴルーチン
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM) // SIGINT, SIGTERM を検知する
		<-c

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		slog.Info("Server is shutting down...")
		if err := server.Shutdown(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) { // タイムアウト時の処理を分ける
				slog.Warn("HTTP server Shutdown: timeout")
			} else {
				slog.Error("HTTP server Shutdown: ", err)
			}
			close(idleConnsClosed) // エラーが発生した場合は強制終了
			return
		}
		slog.Info("Server is shut down")
		close(idleConnsClosed) // Shutdown処理が完了したらチャネルを閉じる
	}()

	slog.Info("Server is running on ", addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server ListenAndServe: ", err)
	}

	<-idleConnsClosed // Shutdown処理が完了するまで待機する
}
