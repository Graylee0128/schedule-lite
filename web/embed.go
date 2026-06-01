// Package web 內含前端靜態檔,用 embed 打包進 binary,跟後端同源提供
//(免 CORS、單一 binary 部署)。依使用者分兩個子目錄:
//   admin/  管理台(店長)        → 入口 /
//   staff/  員工填班頁           → 入口 /a/{token}
//   style.css 兩頁共用;所有資源統一由 /static/ 服務(見 cmd/server/main.go)。
package web

import "embed"

// Files 內含 admin/、staff/ 與共用 style.css。
//
//go:embed admin staff style.css
var Files embed.FS
