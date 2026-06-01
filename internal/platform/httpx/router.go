// Package httpx 提供 HTTP 共用工具:平台探針 + JSON 輔助函式。
// 各 domain(如 store)自行把功能路由註冊到 mux 上。
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Mount 把平台層探針掛到 mux:
// readiness 是 hook——實際去 ping DB(見 main),不通就回 503。
func Mount(mux *http.ServeMux, log *slog.Logger, ready func() error) {
	// 存活探針:服務還活著就回 200。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// 就緒探針:相依性(DB)正常才回 200,否則 503。
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := ready(); err != nil {
			log.Warn("就緒檢查失敗", "err", err)
			JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}

// JSON 統一輸出 JSON 回應。
func JSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// DecodeJSON 解析請求 body 到 dst;遇到未知欄位也視為錯誤(嚴格驗證輸入)。
func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// Error 輸出統一錯誤格式 {"error": msg}。
func Error(w http.ResponseWriter, code int, msg string) {
	JSON(w, code, map[string]string{"error": msg})
}
