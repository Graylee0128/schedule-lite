// Package web 內含前端靜態檔,用 embed 打包進 binary,跟後端同源提供
//(免 CORS、單一 binary 部署)。依使用者分兩個子目錄:
//   admin/  管理台(店長)        → 入口 /
//   staff/  員工填班 /a/{token}、看自己班表 /s/{token}
//   style.css 共用樣式;dragGrid.js 共用拖曳塗選元件;資源統一由 /static/ 服務(見 cmd/server/main.go)。
package web

import "embed"

// Files 內含 admin/、staff/ 與共用的 style.css、dragGrid.js。
//
//go:embed admin staff style.css dragGrid.js
var Files embed.FS
