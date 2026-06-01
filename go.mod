// schedule-lite:輕量門市排班輔助系統(模組根)
module schedule-lite

go 1.22

// 直接相依;其餘 indirect 與 go.sum 由 `go mod tidy` 補齊。
require (
	github.com/jackc/pgx/v5 v5.6.0
	github.com/pressly/goose/v3 v3.21.1
)
