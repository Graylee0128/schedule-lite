package pg

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	// 註冊 "pgx" 這個 database/sql driver,給 goose 用。
	_ "github.com/jackc/pgx/v5/stdlib"

	"schedule-lite/db"
)

// Migrate 在服務啟動時套用所有尚未執行的 migration。
//
// 為什麼用 database/sql 而不是 pgxpool:goose 的 API 吃 *sql.DB。
// 這個連線只在啟動時用一次、用完就關;App 執行期查詢仍走 pgxpool(見 pg.go)。
func Migrate(dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("開啟 migration 連線失敗: %w", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(db.MigrationsFS) // 用 embed 進來的 .sql,而非磁碟路徑
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("設定 goose dialect 失敗: %w", err)
	}
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		return fmt.Errorf("套用 migration 失敗: %w", err)
	}
	return nil
}
