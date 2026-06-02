// Command server 是 schedule-lite HTTP 服務的進入點。
//
// 啟動流程:載入設定 → 套用 migration → 連線池 → 組裝路由(探針 + 各 domain)→
// 開始服務 → 等 SIGTERM 優雅關閉。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"schedule-lite/internal/platform/config"
	"schedule-lite/internal/platform/httpx"
	"schedule-lite/internal/platform/pg"
	"schedule-lite/internal/store"
	"schedule-lite/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("服務以錯誤結束", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)

	// 先套用資料庫 migration(schema 沒就緒就別啟動)。
	log.Info("套用資料庫 migration")
	if err := pg.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}

	// 建立執行期用的連線池。
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dbCancel()
	pool, err := pg.Connect(dbCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("資料庫連線就緒")

	// 就緒 hook:實際去 ping 連線池;DB 不通就回 503,k8s 不會把流量導進來。
	ready := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return pool.Ping(ctx)
	}

	// 組裝路由:平台探針 + 各 domain 的功能路由 + 靜態前端。
	mux := http.NewServeMux()
	httpx.Mount(mux, log, ready)
	store.NewHandler(store.NewRepository(pool), log).RegisterRoutes(mux)

	// 前端(同源):靜態資源統一掛在 /static/(對應 web/ 內所有檔),
	// 兩個頁面入口各自指到對應 HTML。
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(web.Files)))
	// 管理台首頁(店長):只精確匹配根路徑。
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web.Files, "admin/index.html")
	})
	// 員工填班頁:magic-link /a/{token} 一律回同一份 HTML,頁面 JS 再用 token 打 /api/availability。
	mux.HandleFunc("GET /a/{token}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web.Files, "staff/availability.html")
	})
	// 員工看自己班表頁(v2):/s/{token},JS 用 token 打 /api/my-schedule。
	mux.HandleFunc("GET /s/{token}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web.Files, "staff/schedule.html")
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 監聽 OS 終止訊號,讓容器 / k8s 能乾淨地把我們關掉。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("服務開始監聽", "addr", cfg.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		// ListenAndServe 真的出錯(例如 port 被占用)。
		return err
	case <-ctx.Done():
		// 收到關閉訊號,給連線 10 秒排空時間。
		log.Info("收到關閉訊號,開始排空連線")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// newLogger 建立 JSON 結構化 logger;無法解析的等級退回 info。
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
