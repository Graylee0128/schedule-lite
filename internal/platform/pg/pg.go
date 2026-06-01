// Package pg 負責 PostgreSQL 連線:建立 pgx 連線池、健康檢查、關閉。
package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect 用 DSN 建立 pgx 連線池,並先 ping 一次確認連得上。
// 連不上就回錯,讓服務啟動階段就 fail-fast,而不是等到第一個請求才爆。
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("建立連線池失敗: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping 資料庫失敗: %w", err)
	}
	return pool, nil
}
