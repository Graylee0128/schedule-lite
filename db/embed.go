// Package db 把 SQL migration 檔透過 embed 打包進 binary,
// 這樣執行檔自帶 migration,不需另外把 .sql 複製到部署環境。
package db

import "embed"

// MigrationsFS 內含 migrations/ 底下所有 .sql,交給 goose 套用。
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
