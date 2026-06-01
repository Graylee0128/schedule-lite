# schedule-lite

針對小型店家 / 加盟店的 **availability-first 智慧排班輔助系統**。

> 店長設定每天需要哪些班別、各需幾人 → 員工用**免註冊連結**填「每週可上的時段」→
> 店長一頁看到**缺口 heatmap**:哪些班別缺人、誰還沒填。

純 Go 後端(標準庫 `net/http` + PostgreSQL),前端是極簡 HTML/CSS/JS(前後端分離 / CSR),
整包 `embed` 進**單一 distroless binary**,`docker compose` 一鍵起。

## 功能

- 組織 / 門市 / 員工 / 班別模板 CRUD(建門市自動帶 早 / 中 / 晚 / 大夜 4 班別)
- **員工填班**:magic-link(免註冊、免登入),手機友善的「點一下循環」四態色塊
- **缺口分析 heatmap**:每個(班別 × 星期)顯示 需求 vs 可上人數、未填名單
- 同源 JSON API、liveness / readiness 探針、結構化日誌、優雅關閉

## 技術

Go 1.22 · PostgreSQL 16 · pgx v5 · goose migrations · Docker(multi-stage、distroless static / nonroot)· docker compose

> 目前限制(MVP):`/api/*`(店長端)尚無身分驗證;員工端靠 magic-link token 認證。

---

## 一鍵啟動(乾淨 Linux,只要 Docker)

貼這行 —— 自動 **抓 repo → build → 起 app + postgres**,完成後開 <http://localhost:8080/>:

```bash
curl -fsSL https://raw.githubusercontent.com/Graylee0128/schedule-lite/main/get.sh | bash
```

- 只依賴 **Docker + curl**;沒裝 Go 也行(腳本會用 `golang:1.22` 容器產 `go.sum`)。
- **8080 被佔用會自動換 port**(往上找空的),腳本最後會印出實際網址,照著開即可。
  - 想指定 port:`... | APP_PORT=9000 bash`。
- **可安全重跑**:中途失敗(網路斷、port 衝突等)就再貼一次同一行,會自動清理重來,不會把事情搞砸。
  - 安裝位置預設 `~/schedule-lite`,可用 `WORKDIR=...` 改;非本工具建立的同名資料夾不會被覆蓋(要覆蓋加 `FORCE=1`)。
- 想先看腳本再跑(建議):
  ```bash
  curl -fsSL https://raw.githubusercontent.com/Graylee0128/schedule-lite/main/get.sh -o get.sh
  less get.sh && bash get.sh
  ```
- 收掉:`cd ~/schedule-lite && docker compose down`(加 `-v` 連 DB 資料清)。

> 已經 `git clone` 的話,也可以直接 `bash scripts/deploy.sh`(同樣會自動處理 `go.sum`)。

## 開畫面測試一遍

開 <http://localhost:8080/>(管理台),依序:

1. **建組織**(或從下拉選既有)→ **建門市**(自動帶 4 班別)→ 選門市
2. **建員工** → 點該員工「**發填班連結**」→ 複製出現的 `http://localhost:8080/a/<token>`
3. **開新分頁貼上那條連結**(模擬員工,可用手機 / DevTools 手機模擬)→
   點格子切換意願(🟩 非常想上 / 🟨 可配合 / 🟥 絕對不行)→ 按「**儲存我的時段**」
4. 回管理台第 5 區按「**重新整理**」→ 看**缺口 heatmap**:剛填的格變綠、無人可上的紅、未填名單列在下方

> ⚠️ 填班連結綁「**產生當下選的那間門市**」。要在大表看到缺口,請確認**管理台正在看的門市 = 連結所屬門市**。

### (選用)直接看資料庫 — Adminer

```bash
docker compose --profile tools up -d adminer   # 開 http://localhost:8081
#   System = PostgreSQL   Server = db
#   Username / Password / Database = schedule / schedule / schedule_lite
```

> Adminer 用 compose profile 關著,預設不啟動。它是 DB 管理介面,只在本機 / 內網用,別曝露到公網。

---

## API 速覽

同源 JSON API,前端與 curl 共用。

| 路由 | 說明 |
|---|---|
| `GET /healthz` · `GET /readyz` | liveness / readiness 探針 |
| `POST` · `GET /api/organizations` | 建立 / 列出組織 |
| `POST /api/stores` · `GET /api/stores?organization_id=` | 建立 / 列出門市 |
| `POST /api/employees` · `GET /api/employees?organization_id=` | 建立 / 列出員工 |
| `GET/POST /api/shift-templates` · `PUT/DELETE /api/shift-templates/{id}` | 班別模板 CRUD |
| `POST /api/access-links` | 產生員工填班 magic-link `{employee_id, store_id}` → `{token, url}` |
| `GET/PUT /api/availability?token=` | 員工讀取 / 整批覆寫可上時段 |
| `GET /api/coverage?store_id=` | 缺口分析(每格 需求 vs 可上、未填名單) |

範例:

```bash
BASE=http://localhost:8080
ORG=$(curl -sS -X POST $BASE/api/organizations -H 'Content-Type: application/json' -d '{"name":"胖老爹"}')
ORG_ID=$(echo "$ORG" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
curl -sS -X POST $BASE/api/stores -H 'Content-Type: application/json' \
  -d "{\"organization_id\":\"$ORG_ID\",\"name\":\"板橋店\"}"
```

## 設定(環境變數)

| 變數 | 預設 | 說明 |
|---|---|---|
| `SL_ADDR` | `:8080` | HTTP 監聽位址 |
| `SL_LOG_LEVEL` | `info` | 日誌等級 debug/info/warn/error |
| `SL_ENV` | `dev` | 執行環境 dev/prod |
| `SL_DATABASE_URL` | `postgres://schedule:schedule@localhost:5432/schedule_lite?sslmode=disable` | Postgres 連線字串(compose 內覆寫 host 為 `db`) |

## 目錄結構

```text
get.sh                      一鍵安裝腳本(curl | bash 用)
cmd/server/                 進入點(main + 連線 + migration + 路由 + 優雅關閉)
internal/
  platform/
    config/                 env 設定載入
    httpx/                  探針 + JSON 輔助
    pg/                     pgx 連線池 + goose migration 執行
  store/                    組織/門市/員工/班別/填班/缺口 domain
db/
  embed.go                  把 migration 檔 embed 進 binary
  migrations/               SQL migration(goose 格式)
web/                        前端(CSR,依使用者分子目錄)
  admin/                    管理台(店長),入口 /
  staff/                    員工填班頁,入口 /a/{token}
  style.css                 共用樣式(資源由 /static/ 服務)
  embed.go                  把前端 embed 進 binary,同源服務
scripts/
  deploy.sh                 build + 起 compose + 驗證探針
  teardown.sh               收掉 compose(-v 連資料清)
Dockerfile                  多階段 distroless
docker-compose.yml          app + postgres(+ 選用 adminer)
```
