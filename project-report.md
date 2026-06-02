## 0. 總覽

### 它是什麼
針對小型店家 / 加盟店的 **availability-first 排班輔助系統**:店長設定「每天哪些班別、各要幾人」→ 員工用**免註冊連結**填「每週可上時段」→ 店長一頁看**缺口 heatmap**(哪裡缺人、誰沒填)。

### 一句話架構
**模組化單體(modular monolith)**:單一 Go binary,標準庫 `net/http` 提供 JSON API + 同源 embed 前端,後接 PostgreSQL。整包 `docker compose` 起。

```
瀏覽器 (admin / staff 前端, CSR)
   │  fetch  /api/*  (同源, 免 CORS)
   ▼
Go binary (net/http ServeMux)
   ├─ /healthz /readyz          平台探針
   ├─ /api/*                    store domain handlers
   ├─ /static/*                 embed 前端靜態資源
   ├─ /  /a/{token}             兩個頁面入口
   │
   ├─ internal/store            業務邏輯 (handler → repo)
   └─ internal/platform/pg      pgxpool
                                   │
                                   ▼
                           PostgreSQL 16
```

### 請求生命週期(以「員工存可上時段」為例)
1. 員工頁 `PUT /api/availability?token=XXX`(body: slots 陣列)。
2. `ServeMux` 依 method+path 命中 `handler_availability.putAvailability`。
3. handler 用 `ResolveToken(token)` 反查員工 + 門市(token 只比對 hash)。
4. 驗證每筆 slot(weekday 0–6、preference 0–2)。
5. `repo.ReplaceAvailability`:在一個 transaction 內「刪該員工該店所有 weekday 時段 → 重新插入」。
6. 回 `{"saved": N}`。

### 技術選型(與理由)
| 層 | 選型 | 理由 |
|---|---|---|
| 語言 / HTTP | Go 1.22 `net/http` | Go 1.22 原生 method+path 路由,不必引第三方 router |
| DB 存取 | pgx v5(pgxpool) | Postgres 原生驅動,效能與型別好;查詢目前**手寫**(sqlc 列為之後重構) |
| migration | goose v3 | 啟動時自動套用;SQL 檔 `embed` 進 binary |
| 前端 | 純 HTML/CSS/JS(CSR) | 前後端分離、免框架;同源 embed 免 CORS |
| 封裝 | Docker 多階段 + distroless | 單一靜態 binary、非 root、攻擊面小 |

> **v1.5 階段 A 變更摘要(2026-06-02,✅ 已驗證)**:token 改綁**員工**(一人一條、可填多店);啟用 `employee_store_memberships`(建員工預設加入全組織門市);新增 `/api/me`(列可填門市)、`/api/memberships`(店長調歸屬)、`availability_submissions`(提交標記);`/api/availability` 改需 `store_id`;缺口的「未填名單」改依 membership + 提交標記。下方逐檔說明中「員工填班 / token / 缺口」相關處以此摘要為準,驗證通過後再完整併入各節。
>
> **v1.5 階段 B 變更摘要(2026-06-02,✅ homelab curl + 桌面 UI 驗證;手機觸控待實機)**:availability 與需求**改逐小時**,固定 4 班別模板淘汰。新增 `stores.open_hour/close_hour`(營業時段)、`staffing_requirements`(逐小時需求人數);`availability_slots` 重建為 `(員工,店,weekday,hour,preference 1/2)`**只存正向**(未塗=絕對不行不落 DB);`DROP TABLE shift_templates`。API:移除 `/api/shift-templates*`,新增 `/api/store-hours`、`/api/requirements`;`/api/availability` 與 `/api/coverage` 改逐小時形狀(coverage = 「小時 × 星期」)。前端新增 `web/dragGrid.js`(when2meet 式拖曳塗選,員工填班與老闆設需求共用),員工/管理台頁面隨之重寫。下方逐檔說明中凡提到「班別模板 / 7×4 / 三元偏好」者,以本摘要為準。
>
> **v2 變更摘要(2026-06-02,✅ homelab curl + go test + 桌面 UI 驗證)**:加入排班指派 + Rule Engine + 發布 + 簡化雙階段。粒度=**逐小時指派**(不另設 scheduled_shift,需求坑=staffing_requirements、指派=`(version,employee,weekday,hour)`);週模型=**循環週 + 版本快照**(draft→published,改已發布開新 draft 複製自 published)。新增表:`schedule_versions`、`shift_assignments`、`assignment_issues`、`employees.max_weekly_hours`。Rule Engine(`internal/store/rules.go`,純函式 + `rules_test.go`)做 4 檢查:不可用(硬)、跨店雙排(硬)、人數未滿(黃)、超週工時(黃);技能延後。新 API:`/api/schedule`、`/api/schedule/assignments`、`/api/schedule/publish`(硬違反→409)、`/api/schedule/export`(CSV)、`/api/employee-availability`、`/api/my-schedule`、`/api/my-schedule/issues`;新員工頁 `/s/{token}`。修正 `web/embed.go` 漏 embed `dragGrid.js`。
>
> **v3 階段 A 變更摘要(2026-06-02,✅ homelab curl 驗證)**:一鍵建議班表(預排推薦)。`internal/store/autofill.go`(純函式 `SuggestSchedule` + `autofill_test.go`):候選 = 標可上 + 沒跨店占用(**只用候選人 → 零硬衝突**),計分(偏好高 > 本週時數少 > 穩定)且尊重 `max_weekly_hours`,**瓶頸優先**裝箱(候選最少的格先排)。新 API `POST /api/schedule/autofill`(整批覆寫 draft);管理台第 6 區「🪄 一鍵建議排班」鈕。實測:`suggested:2`、`publishable:true`、發布 200。
>
> **v3 階段 B 變更摘要(2026-06-02,✅ homelab curl 驗證)**:完整兩階段提交。狀態機 `draft → published(確認中)→ locked(定案)`。`db/migrations/0006_two_phase.sql`:status CHECK 加 `locked`、`schedule_versions.confirm_deadline`、新表 `shift_confirmations(version,employee,status pending/confirmed/declined,reason)`。發布時 seed confirmations(pending)+ `confirm_deadline=now()+24h`;員工 `/s/{token}`「✅ 接受整週」→ confirmed、「回報某格」=回絕(declined+理由,沿用 `assignment_issues`);老闆第 6 區看每位確認狀態 + 倒數,按「🔒 鎖定」定案。**24h 採軟截止 + 老闆手動鎖**(不靠背景排程)。新 API:`POST /api/schedule/lock`、`POST /api/my-schedule/confirm`。實測:publish 200 → confirm 204 → 老闆端 confirmed → lock 200 → 鎖後 confirm 擋 400。
>
> **issue #1 UI 參考圖 — 取捨決策(2026-06-02)**:協作者提兩個方向 ——(a)員工依「權重」填可上時段、讓系統自動分配做衝突處理;(b)跨日/過夜班。決策:**先做「衝突可視化報告」**(誰/何時/哪種/撞哪家店 + 建議動作;資料 Rule Engine 已備,主要補跨店店名 + 前端表格)。**權重**——現有逐小時兩級偏好(可配合/非常想上)已是輕量權重且 autofill 已用之計分,完整權重需「跨店全域排班器」才有意義(現為每店獨立排班、誰先排誰贏),對「單店換 Excel」屬過度設計且增員工認知負荷,故 **跨店權重全域分配 + 跨日班標記為連鎖店/未來階段,癥結交協作者 fork 出去實驗,有成果再評估併入**。

---

## 1. `cmd/server/` — 進入點

### `main.go`
唯一的 `package main`。`main()` 只做一件事:呼叫 `run()`,有錯就 `slog.Error` + `os.Exit(1)`(把實際邏輯放 `run()` 是為了能用 `defer`、回傳 error)。

**`run()` 的啟動順序(每一步都有意義)**:
1. `config.Load()` — 讀環境變數設定。
2. `newLogger(cfg.LogLevel)` — 建 JSON 結構化 `slog` logger,設為 default。等級無法解析時退回 info。
3. `pg.Migrate(cfg.DatabaseURL)` — **先套用 migration**;schema 沒就緒就不啟動(fail-fast)。
4. `pg.Connect(ctx, dsn)` — 建執行期 `pgxpool` 連線池(帶 10s timeout),`defer pool.Close()`。
5. 定義 `ready` 閉包 — `/readyz` 的鉤子,實際 `pool.Ping`(2s timeout);DB 不通就回 503。
6. 組路由(`http.NewServeMux`):
   - `httpx.Mount(mux, log, ready)` 掛 `/healthz` `/readyz`。
   - `store.NewHandler(store.NewRepository(pool), log).RegisterRoutes(mux)` 掛所有 `/api/*`。
   - `GET /static/` → `http.StripPrefix("/static/", http.FileServerFS(web.Files))` 服務 embed 靜態資源。
   - `GET /{$}` → `admin/index.html`(`{$}` 精確匹配根路徑)。
   - `GET /a/{token}` → `staff/availability.html`(token 由前端 JS 自網址取出再打 API)。
7. 建 `http.Server`,設四個 timeout(ReadHeader 5s / Read 15s / Write 15s / Idle 60s)防慢速連線。
8. 在 goroutine 內 `ListenAndServe`;主線用 `signal.NotifyContext(SIGINT, SIGTERM)` 等訊號。
9. **優雅關閉**:收到訊號後 `srv.Shutdown(10s)` 排空連線;`ListenAndServe` 真出錯(如 port 被占)則直接回該錯。

> **路由優先序**:Go 1.22 ServeMux 以「最具體 pattern」優先,所以 `/healthz`、`/api/*`、`/static/`、`/a/{token}` 都比 `GET /{$}` 具體,不會被吃掉。

---

## 2. `internal/platform/` — 與業務無關的基礎設施

### `config/config.go`
12-factor 風格設定。`Config` 結構四欄:`Addr` / `LogLevel` / `Env` / `DatabaseURL`。
- `Load()` 用 `envOr(key, fallback)` 逐欄讀,缺值套預設;**不 panic**。
- 驗證:`Addr`、`DatabaseURL` 不可為空字串,否則回 error。
- `DatabaseURL` 預設 `postgres://schedule:schedule@localhost:5432/...`(compose 內把 host 覆寫成 `db`;預設指 localhost 是方便 host 上 `go run` 連 compose 的 postgres)。

| 環境變數 | 預設 | 說明 |
|---|---|---|
| `SL_ADDR` | `:8080` | 監聽位址 |
| `SL_LOG_LEVEL` | `info` | 日誌等級 |
| `SL_ENV` | `dev` | 執行環境 |
| `SL_DATABASE_URL` | 見上 | Postgres DSN |

### `httpx/router.go`
HTTP 共用工具,不含業務。
- `Mount(mux, log, ready)`:註冊 `GET /healthz`(永遠 200 `{"status":"ok"}`)與 `GET /readyz`(呼叫 `ready()`,有錯記 warn 並回 503 `not ready`,否則 200 `ready`)。**liveness vs readiness 分離**:healthz 證明「行程活著」、readyz 證明「相依(DB)就緒」,對接 k8s 探針語意。
- `JSON(w, code, body)`:設 `Content-Type`、寫狀態碼、`json.NewEncoder` 輸出。
- `DecodeJSON(r, dst)`:`json.Decoder` + **`DisallowUnknownFields()`**(嚴格,未知欄位視為錯誤)。
- `Error(w, code, msg)`:統一錯誤格式 `{"error": msg}`。

### `pg/pg.go`
- `Connect(ctx, dsn)`:`pgxpool.New` 建池,接著 `Ping`(5s timeout)。ping 失敗就 `Close` 並回錯 → **啟動階段就 fail-fast**,而非等第一個請求才爆。

### `pg/migrate.go`
- `Migrate(dsn)`:開一條 `database/sql`(透過 `pgx/v5/stdlib` 註冊的 `"pgx"` driver)。
- **為什麼用 `database/sql` 而非 pgxpool**:goose 的 API 吃 `*sql.DB`。這條連線只在啟動套 migration 用一次、用完 `Close`;執行期查詢仍走 pgxpool。
- `goose.SetBaseFS(db.MigrationsFS)`(用 embed 的 SQL,不依賴磁碟路徑)→ `SetDialect("postgres")` → `goose.Up(sqlDB, "migrations")`。

---

## 3. `internal/store/` — 唯一的業務 domain

> 命名:目前所有業務都在 `store`(組織/門市/員工/班別/填班/缺口)。之後若膨脹可再拆 domain;現階段聚一處降複雜度。

### `models.go` — 資料結構(API 的 JSON 形狀)
- `Organization`(id/name/timezone/week_start/created_at)、`Store`、`Employee`(`Phone *string`,可 NULL)。
- `ShiftTemplate`:`StartLocal`/`EndLocal` 用 **string `"HH:MM:SS"`**(Go 端不引時間型別,SQL 端轉型);`RequiredSkills []string`(對應 jsonb);`RequiredHeadcount int`。
- `IDName`:只回 id+name 的精簡視圖(員工填班頁用,不外洩多餘欄位)。
- `AvailabilitySlot`(shift_template_id/weekday/preference_level)。
- `AvailabilityContext`:員工填班頁一次性資料包(employee + store + templates + slots)。
- `AvailabilityCount` / `CoverageCell` / `Coverage`:Step 6 缺口分析的中間統計與輸出。

### `repo.go` — 資料存取(手寫 pgx)
- `Repository{pool}`,`NewRepository(pool)`。
- **關鍵手法:SQL 轉型**。輸出用 `id::text`,輸入用 `$1::uuid` / `$3::time`。如此 Go 端 UUID/時間一律用 `string`,不必引 uuid 型別、不必處理 pgx 時間型別,壞 UUID/時間由 PG 報錯碼(再翻成 400)。
- `defaultShiftTemplates`:每店建立時 seed 的 4 班別(早 06–12 / 中 12–18 / 晚 18–24 / 大夜 00–06,各 1 人)。
- `CreateStore`:**transaction** —— 插入門市 + 迴圈插入 4 預設班別,全成功才 commit(`defer tx.Rollback` 已 commit 時為 no-op)。
- `CreateOrganization` / `ListOrganizations` / `ListStores` / `CreateEmployee`(phone 可 nil)/ `ListEmployees`。
- 班別模板:`shiftTemplateCols` 常數(含 `created_at`);`ListShiftTemplates`(`ORDER BY start_local`)、`CreateShiftTemplate`、`UpdateShiftTemplate`(找不到 id 回 `pgx.ErrNoRows`)、`DeleteShiftTemplate`(看 `RowsAffected`)。
- jsonb 技能:`marshalSkills`(nil → `[]`)/ `unmarshalSkills`;`scanShiftTemplate` 把 jsonb bytes 還原成 `[]string`。`rowScanner` 介面讓同一掃描器吃 `Row` 與 `Rows`。

### `repo_availability.go` — magic-link token + 可上時段
- **token 設計(安全核心)**:`newToken()` 用 `crypto/rand` 產 32 bytes → base64url 當「原始 token」;`hashToken` 取其 SHA-256 hex。**DB 只存 hash**,原始 token 不落地 → DB 外洩也無法反推連結。
- `CreateAccessToken(employeeID, storeID)`:存 hash,回原始 token(只此刻回一次)。此版不設過期。
- `ResolveToken(raw)`:`token_hash` 比對 + `revoked_at IS NULL` + 未過期,JOIN 出 employee/store 精簡資料;無效一律回 `ErrNoRows`。
- `GetAvailability` / `ReplaceAvailability`:後者在 transaction 內「刪該員工該店所有 weekday 時段 → 重插」→ **整批覆寫**,前端只要送當下整張表,冪等。

### `repo_coverage.go` — 缺口統計
- `AvailabilityCounts(storeID)`:`count(*) FILTER (WHERE preference_level = n)` 依 (班別, 星期) 聚合出 want/ok/no 人數。
- `NotFilledEmployees(storeID)`:「拿過這店連結但一格都沒填」的員工。沒有 membership,所以用「發過 access token」當「被期待要填」的依據。

### `handler.go` — HTTP 介面 + 路由註冊 + 錯誤翻譯
- `Handler{repo, log}`;`RegisterRoutes(mux)` 一次掛所有路由。
- 各 handler 模式:`DecodeJSON` → `TrimSpace` + 必填檢查 → 呼叫 repo → `JSON` 回應 / `writeDBError`。空清單回 `[]` 而非 `null`。
- **`writeDBError`**:把 Postgres 錯誤碼翻成友善 4xx —— `pgx.ErrNoRows`→404、`22P02`(壞 UUID)→400、`22007/22008`(壞時間)→400、`23503`(FK)→400、`23514`(CHECK,如 weekday/preference 超界)→400;其餘 500。

### `handler_availability.go` — 員工填班兩端
- `createAccessLink`(店長端):`{employee_id, store_id}` → `{token, url:"/a/<token>"}`。
- `getAvailability`(員工端,token 在 query):`ResolveToken` → 取 templates + slots → 回 `AvailabilityContext`。
- `putAvailability`:`ResolveToken` → 驗 weekday/preference → `ReplaceAvailability` → `{"saved": N}`。
- `writeTokenError`:token 無效/過期一律 **401**(友善訊息),並記 warn。

### `handler_coverage.go` — 缺口分析組裝
- `getCoverage(store_id)`:取 templates + `AvailabilityCounts` + `NotFilledEmployees` → 把統計索引成 map → 對「每班別 × 7 天」展開成 28 格,算 `available = want + ok`、`gap = required - available` → 回 `Coverage`。

---

## 4. `db/` — schema 與 migration

### `embed.go`
`//go:embed migrations/*.sql` 把 SQL 檔包進 binary(`var MigrationsFS embed.FS`),migrate.go 用它套用 → **單一 binary 自帶 schema**,部署不必另外送 SQL。

### `migrations/0001_init.sql` — v1 基礎 8 張表
goose 格式(`-- +goose Up/Down`)。四層:
- **租戶層**:`organizations`(timezone、week_start)、`stores`(FK org, ON DELETE CASCADE)。
- **人員層**:`employees`(綁 org,不死綁單店)、`employee_skills`、`employee_store_memberships`(多對多 + role CHECK)、`employee_access_tokens`(只存 `token_hash`,可過期/撤銷)。
- **需求層**:`shift_templates`(start/end `time`、`required_headcount` CHECK ≥0、`required_skills jsonb`)。
- **供給層**:`availability_slots`(weekday **或** specific_date 二擇一的 CHECK、`preference_level` 0–2 CHECK)。
> 排班記錄層 / 考勤層因前向相依,留待後續 migration。

### `migrations/0002_access_token_store.sql`
`ALTER TABLE employee_access_tokens ADD COLUMN store_id`(+索引)。Step 5 需要:連結要綁「哪間店」,員工點開才知填哪些班別。

### ER 摘要
```
organizations 1─* stores 1─* shift_templates
organizations 1─* employees 1─* employee_access_tokens *─1 stores
employees *─* stores (employee_store_memberships)
availability_slots: employee × store × shift_template × (weekday|date) + preference
```

---

## 5. `web/` — 前端(CSR,同源)

> 依使用者分子目錄;資源統一由 `/static/` 服務,兩個頁面入口 `/`(管理台)、`/a/{token}`(員工)。

### `embed.go`
`//go:embed admin staff style.css` 把整個前端包進 binary。

### `admin/`(店長,入口 `/`)
- `index.html`:5 區(組織 / 門市 / 員工 / 班別模板 / 週缺口),資源引 `/static/...`。
- `app.js`:
  - `api(method, path, body)` fetch 封裝(把 `{"error"}` 轉例外)。
  - **狀態持久化(P0-1)**:`loadOrganizations` 撈既有組織下拉;選定的 org/store 存 `localStorage`(只記指標,真資料回 DB),`init()` 重整後自動還原。
  - CRUD:建組織/門市/員工、班別需求人數 PUT、「發填班連結」(`makeLink` 產生並自動複製)。
  - **缺口 heatmap(Step 6)**:`loadCoverage` 打 `/api/coverage` → `renderCoverage` 上色(`cov-ok` 綠 / `cov-short` 黃 / `cov-none` 紅),格內 `可上/需求`,下方未填名單;「重新整理」鈕。

### `staff/`(員工,入口 `/a/{token}`)
- `availability.html`:含 `#loading`、`#fatal`(常駐錯誤區)、`#meSection`。
- `availability.js`:
  - token 從 `location.pathname.split("/a/")[1]` 取出。
  - **四態色塊(手機友善)**:`STATES`(未填→非常想上→可配合→絕對不行,白/綠/黃/紅循環),`prefCell` 點一下切下一態,狀態存 `dataset.level`。
  - **手機轉置(P0-2)**:`renderGrid` 把「天」做成列、班別做成欄(≤4 欄,塞得進手機,直向捲完一週)。
  - **常駐狀態(P1-3)**:`showFatal` 把壞 token / 載入失敗顯示為**不消失**的錯誤,而非 4 秒 toast 後留白。
  - 送出:`collectSlots` 蒐集非「未填」格 → `PUT`(整批覆寫)。

### `style.css`
共用樣式 + `.pref-*`(填班色塊,`min-height:44px` 觸控)+ `.cov-*`(缺口 heatmap)+ `.pref-legend`(圖例)。`#fillGrid` 內 scoped 樣式不影響管理台。

---

## 6. `scripts/` — 操作腳本

### `deploy.sh`
就地部署:`BASH_SOURCE` 解析專案根(任意 CWD 可跑)→ 檢查 docker/curl → 偵測 `docker compose`(v2)/`docker-compose`(v1)→ **缺 `go.sum` 就 `go mod tidy`**(需 Go)→ `compose up --build -d` → 輪詢 `/healthz`(最多 30s)→ 印探針結果。

### `teardown.sh`
`down`(保留 DB volume)或 `-v`(連 `pgdata` 一起刪)。同樣自動解析專案根、偵測 compose。

### `get.sh`(公開 repo 的一鍵安裝,供 `curl | bash`)
- 整段包進 `main()`,**檔尾才 `main "$@"`** → curl 半截下載不會執行殘缺邏輯。
- 防呆:檢查 curl/tar/docker/daemon;**WORKDIR 守衛**(拒絕 `/`、`$HOME`;非本工具建立的同名資料夾不覆蓋,除非 `FORCE=1`)。
- 流程:tarball 抓 repo(免 git)→ 沒 Go 就用 `golang:1.22` 容器產 `go.sum` → `compose down`(清殘留)→ **自動挑空 port**(8080/5432 被占就往上找)→ `up --build` → 輪詢 healthz → 印實際網址。
- **可安全重跑**:每次 `rm -rf` 重抓 + `down` 清殘留;錯誤 trap 提示「重貼同一行即可」。

---

## 7. 封裝與編排

### `Dockerfile`(多階段)
- **build 階段**(`golang:1.22-alpine`):先 `COPY go.mod go.sum` + `go mod download`(利用 layer 快取)→ `COPY . .` → `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"` 產靜態執行檔。
- **執行階段**(`distroless/static-debian12:nonroot`):只 COPY binary、`USER nonroot`、`ENTRYPOINT ["/server"]`。無 shell、無套件管理器 → 攻擊面最小。
> 注意:`COPY go.mod go.sum` 需要 `go.sum` 存在,故首次部署要先有它(deploy.sh / get.sh 會處理)。

### `docker-compose.yml`
- `app`:`build: .`,host port `${APP_PORT:-8080}:8080`(可覆寫),SL_* 環境變數,`depends_on db: condition: service_healthy`(等 DB 健康才起)。
- `db`:`postgres:16-alpine`,`${DB_PORT:-5432}`,`pgdata` 具名 volume(重啟不掉資料),healthcheck `pg_isready`。
- `adminer`:`profiles:["tools"]`(預設不起;`--profile tools` 才開),`${ADMINER_PORT:-8081}:8080`,DB GUI。

---

## 8. 端到端資料流走查

```
店長(管理台 /)
  建組織 ── POST /api/organizations
  建門市 ── POST /api/stores         → 同 tx seed 4 班別
  建員工 ── POST /api/employees
  發連結 ── POST /api/access-links   → 回 /a/<token>(DB 存 hash)
        │
員工(/a/<token>)
  開頁   ── GET  /api/availability?token=  → 看自己/門市/班別
  填存   ── PUT  /api/availability?token=  → ReplaceAvailability(整批覆寫)
        │
店長(管理台 第5區)
  看缺口 ── GET  /api/coverage?store_id=   → 28 格 需求vs可上 + 未填名單
```
