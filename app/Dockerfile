# ベースステージ: 最新のGoイメージを使用してビルド
FROM golang:1.22.5-bookworm AS builder

# アプリケーションディレクトリを作成
WORKDIR /app

# ソースコードをコピー
COPY . .

# Go Modulesの初期化と依存関係の追加
RUN if [ ! -f go.mod ]; then go mod init myapp; fi
RUN go mod tidy

# アプリケーションをビルド
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/myapp

# 実行ステージ: 最新のDebian slimイメージを使用
FROM debian:bookworm-slim

# 実行バイナリをコピー
COPY --from=builder /app/myapp /app/myapp

# コンテナ起動時に実行されるコマンド
CMD ["/app/myapp"]
