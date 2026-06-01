// Package config 從環境變數載入執行期設定(12-factor 風格)。
// 原則:保持精簡,只放「目前這一步真的用得到」的欄位。
package config

import (
	"fmt"
	"os"
)

// Config 是執行期設定。欄位會隨每一步成長;
// Step 2 起多了資料庫連線字串。
type Config struct {
	Addr        string // HTTP 監聽位址,例如 ":8080"
	LogLevel    string // "debug" | "info" | "warn" | "error"
	Env         string // "dev" | "prod"
	DatabaseURL string // Postgres DSN
}

// Load 從環境變數讀設定,缺值時套用合理預設。
// 它不會 panic;選填值缺少時退回預設。
func Load() (Config, error) {
	cfg := Config{
		Addr:     envOr("SL_ADDR", ":8080"),
		LogLevel: envOr("SL_LOG_LEVEL", "info"),
		Env:      envOr("SL_ENV", "dev"),
		// 預設指向本機(compose 會把 host 覆寫成 db);方便 host 上 `go run` 直接連 compose 的 postgres。
		DatabaseURL: envOr("SL_DATABASE_URL", "postgres://schedule:schedule@localhost:5432/schedule_lite?sslmode=disable"),
	}
	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("SL_ADDR 不可為空")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("SL_DATABASE_URL 不可為空")
	}
	return cfg, nil
}

// envOr 讀取環境變數;不存在或為空字串時回傳 fallback。
func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
