# --- 編譯階段 ---
FROM golang:1.22-alpine AS build
WORKDIR /src

# 先複製 go.mod / go.sum 以利 layer 快取。
# go.sum 由 `go mod tidy` 產生(Step 2 加入 pgx、goose 後)。
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# 靜態編譯(關閉 CGO),去除除錯資訊與路徑,縮小執行檔。
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- 執行階段 ---
# distroless static:無 shell、無套件管理器,攻擊面最小;nonroot 以非 root 執行。
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
