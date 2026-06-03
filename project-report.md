## 0. 總覽

### 它是什麼
針對小型店家 / 加盟店的 **排班 copilot**:從「員工盲填 Excel、老闆手動橋」升級成一條可驗證的流水線——

1. 店長設**營業時段**與**逐小時需求人數**(哪個小時要幾人)。
2. 員工用**免註冊連結**,以 when2meet 式拖曳塗選填「每週逐小時可上時段」(兩級偏好)。
3. 店長一頁看**缺口 heatmap**(小時 × 星期:哪裡缺人、誰沒填)。
4. 按「🪄 一鍵建議排班」由系統產草稿 → 微調 → **Rule Engine 驗證**(硬衝突擋發布、軟警告黃標)。
5. **發布 → 員工確認 / 回絕(兩階段)→ 老闆鎖定定案**。

員工從「單向盲填」變「填完看得到結果並能回應」;老闆從「苦工」變「審閱者」。

### 功能範圍
- 組織 / 門市 / 員工 CRUD;員工 ↔ 門市多對多 membership(一員工一連結、可跨店填班)。
- 逐小時營業時段 + 逐小時需求;when2meet 拖曳填班(只存正向意願,未塗=絕對不行)。
- 逐小時缺口分析(小時 × 星期)。
- 排班指派(循環週 + 版本快照);一鍵預排建議(瓶頸優先貪婪 + 計分,保證零硬衝突)。
- Rule Engine 4 檢查:不可用(硬)、跨店同時段雙排(硬)、人數未滿(軟)、超週工時(軟)。
- 兩階段提交:`draft → published(確認中)→ locked(定案)`,軟性 24h 截止 + 老闆手動鎖;員工接受整週 / 回報問題(=回絕)。
- CSV 匯出(UTF-8 BOM);健康/就緒探針;單一 binary 自帶 schema。

> 本報告依程式碼結構逐層說明系統目前的架構與實作。

### 一句話架構
**模組化單體(modular monolith)**:單一 Go binary,標準庫 `net/http` 提供 JSON API + 同源 embed 前端,後接 PostgreSQL。整包 `docker compose` 起。

```
瀏覽器 (admin 管理台 / staff 員工頁, 純 CSR)
   │  fetch  /api/*  (同源, 免 CORS)
   ▼
Go binary (net/http ServeMux)
   ├─ /healthz /readyz          平台探針(liveness / readiness)
   ├─ /api/*                    store domain handlers
   ├─ /static/*                 embed 前端靜態資源
   ├─ /                         管理台(店長)
   ├─ /a/{token}                員工填可上時段頁
   ├─ /s/{token}                員工看自己班表 / 確認頁
   │
   ├─ internal/store            業務邏輯 (handler → repo;rules / autofill 為純函式)
   └─ internal/platform/pg      pgxpool
                                   │
                                   ▼
                           PostgreSQL 16
```

### 請求生命週期(以「員工存逐小時可上時段」為例)
1. 員工頁 `PUT /api/availability?token=XXX&store_id=YYY`(body:逐小時 slots 陣列)。
2. `ServeMux` 依 method+path 命中 `handler_availability.putAvailability`。
3. `ResolveTokenForStore(token, store)` 反查員工 + 驗證他屬於該門市(token 只比對 SHA-256 hash)。
4. 驗證每筆 slot(weekday 0–6、hour 0–23、preference 1/2)。
5. 整批覆寫該員工該店的可上時段(transaction:刪 → 重插),只存有塗到的正向格。
6. 標記該員工該店「已提交」,回 `{"saved": N}`。

### 技術選型(與理由)
| 層 | 選型 | 理由 |
|---|---|---|
| 語言 / HTTP | Go 1.22 `net/http` | Go 1.22 原生 method+path 路由,不必引第三方 router |
| DB 存取 | pgx v5(pgxpool) | Postgres 原生驅動,效能與型別好;查詢目前**手寫**(sqlc 列為之後重構) |
| migration | goose v3 | 啟動時自動套用;SQL 檔 `embed` 進 binary |
| 規則 / 預排 | 純函式(`rules.go` / `autofill.go`) | 不碰 DB,可 table-driven 測試(`*_test.go`) |
| 前端 | 純 HTML/CSS/JS(CSR) | 前後端分離、免框架;同源 embed 免 CORS |
| 封裝 | Docker 多階段 + distroless | 單一靜態 binary、非 root、攻擊面小 |

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
   - `GET /a/{token}` → `staff/availability.html`(員工填可上時段)。
   - `GET /s/{token}` → `staff/schedule.html`(員工看自己班表 / 確認)。token 都由前端 JS 自網址取出再打 API。
7. 建 `http.Server`,設四個 timeout(ReadHeader 5s / Read 15s / Write 15s / Idle 60s)防慢速連線。
8. 在 goroutine 內 `ListenAndServe`;主線用 `signal.NotifyContext(SIGINT, SIGTERM)` 等訊號。
9. **優雅關閉**:收到訊號後 `srv.Shutdown(10s)` 排空連線;`ListenAndServe` 真出錯(如 port 被占)則直接回該錯。

> **路由優先序**:Go 1.22 ServeMux 以「最具體 pattern」優先,所以 `/healthz`、`/api/*`、`/static/`、`/a/{token}`、`/s/{token}` 都比 `GET /{$}` 具體,不會被吃掉。

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

> 命名:目前所有業務都在 `store`(組織/門市/員工/填班/缺口/排班)。之後若膨脹可再拆 domain;現階段聚一處降複雜度。
> 結構慣例:**handler(HTTP)→ repo(pgx)** 分層;**規則與預排抽成純函式**(`rules.go` / `autofill.go`),不碰 DB、可單測。

### `models.go` — 資料結構(API 的 JSON 形狀)
- 註冊資料:`Organization`、`Store`(含 `OpenHour`/`CloseHour` 營業時段)、`Employee`(`Phone *string` 可 NULL)、`IDName`(精簡視圖)。
- 填班 / 缺口(逐小時):`HourSlot`(weekday × hour + `PreferenceLevel` 1=可配合/2=非常想上)、`Requirement`(weekday × hour × headcount)、`StoreHours`、`MeContext`(我是誰 + 可填哪些門市)、`AvailabilityContext`(填班頁資料包)、`HourCount` / `CoverageCell` / `Coverage`(小時 × 星期缺口 + 未填名單)。
- 排班 / 驗證 / 兩階段:
  - `ScheduleVersion`(`status` draft/published/locked、`PublishedAt`、`ConfirmDeadline`)。
  - `ScheduleAssignment`(employee × weekday × hour)、`AvailabilityRow`(內部用,含偏好)、`ScheduleEmployee`(含 `MaxWeeklyHours`)。
  - `Violation`(kind:unavailable/double_booked/overtime;severity hard/soft)、`UnderStaffed`、`ValidationReport`(Hard/Soft/Understaffed/Publishable)。
  - `Confirmation`(員工對某版的 pending/confirmed/declined + 理由)、`ScheduleIssue`(員工標的問題格)。
  - `ScheduleContext`(老闆排班頁一次性包:可編輯的 draft + 最近發布版 + 確認狀態 + 問題)、`HourCell`、`MyScheduleContext`(員工看自己班:published/locked/my_status/deadline + 班格 + 問題)。

### `repo.go` — 註冊資料存取(手寫 pgx)
- `Repository{pool}`,`NewRepository(pool)`。
- **關鍵手法:SQL 轉型**。輸出用 `id::text`,輸入用 `$1::uuid`。如此 Go 端 UUID 一律用 `string`,不必引 uuid 型別;壞 UUID 由 PG 報錯碼(再翻成 400)。`rowScanner` 介面讓同一掃描器吃 `Row` 與 `Rows`。
- `CreateStore`:單純插入門市,**不再 seed 班別**;營業時段用 DB 預設(09–22),老闆之後在管理台調。
- `CreateEmployee`:**transaction** —— 建員工 + 同交易把他加入該組織**所有現有門市**的 membership(`ON CONFLICT DO NOTHING`),預設全店、老闆可增減。
- membership:`ListMembershipStores`(只列 `is_active`)、`AddMembership`(`ON CONFLICT … DO UPDATE is_active=true`,撤回可復原)、`RemoveMembership`。
- 其餘:`CreateOrganization` / `ListOrganizations` / `ListStores` / `ListEmployees`。

### `repo_availability.go` — magic-link token + 逐小時可上時段
- **token 設計(安全核心)**:`crypto/rand` 產 32 bytes → base64url 當「原始 token」;取其 SHA-256 hex 存 DB。**DB 只存 hash**,原始 token 不落地 → DB 外洩也無法反推連結。
- token **綁員工**(非門市):`CreateAccessToken(employeeID)` 回原始 token(只此刻回一次);`ResolveToken` 反查員工;`ResolveTokenForStore` 再 JOIN membership **驗證該員工屬於該店**(跨店填班的權限邊界)。
- `GetAvailability` / `ReplaceAvailability`:逐小時、**只存正向**(未塗=絕對不行,不落 DB);後者在 transaction 內「刪該員工該店所有時段 → 重插」→ 整批覆寫、冪等。成功後 `MarkSubmitted`(upsert 提交標記),供缺口頁分辨「沒回應」vs「提交了但都不行」。

### `repo_settings.go` — 營業時段 + 逐小時需求
- 對應 `GET/PUT /api/store-hours`(讀寫 `stores.open_hour/close_hour`)與 `GET/PUT /api/requirements`(`staffing_requirements` 逐小時需求人數,**整批覆寫**)。需求即「坑」:缺口分析與排班驗證都吃它。

### `repo_coverage.go` — 逐小時缺口統計
- `AvailabilityCounts`:`count(*) FILTER (WHERE preference_level = n)` 依 (weekday, hour) 聚合 want(2)/ok(1) 人數。
- `NotFilledEmployees`:依 **membership + 提交標記** 算「被指派到這店、但還沒提交」的員工(能分辨「沒回應」與「提交了都不行」)。

### `repo_schedule.go` — 版本 / 指派 / 跨店 / 確認 / 問題
- 版本快照:`GetOrCreateDraft`(沒 draft 就開一張,並從最近 published/locked **複製**過來續編)、`LatestPublishedVersion`(status ∈ published/locked)、`PublishDraft`(draft→published,設 `confirm_deadline = now()+24h`)、`LockVersion`(最近 published→locked)。`schedule_versions` 有 **partial unique index** 保證每店至多一張 draft。
- 指派:`ReplaceAllAssignments`(整批覆寫整張 draft,給一鍵預排用)、`ReplaceEmployeeAssignments`(只覆寫某員工那幾格)、`ListAssignments`。
- 跨店:`CrossStoreBusy` 查「同員工同 (weekday,hour) 已在**別店已發布版本**被排」,供 Rule Engine 判跨店雙排。
- 兩階段:`SeedConfirmations`(發布時對該版有班的員工種 pending)、`ListConfirmations`、`SetConfirmation`(upsert confirmed/declined + 理由)、`EmployeeConfirmationStatus`。
- 員工視圖 / 問題:`StoreAvailabilityRows`(含偏好,Rule Engine 與預排共用)、`ListStoreEmployees`、`EmployeeCells` / `EmployeeIssueCells`、`MarkIssue`。

### `rules.go` — Rule Engine(純函式)
- `ValidateSchedule(ScheduleInput) ValidationReport`:不碰 DB,handler 把資料整理成記憶體輸入再呼叫,方便 `rules_test.go` table-driven 測。
- 4 檢查:① **不可用**(指派到員工沒塗可上的格,硬)② **跨店同時段雙排**(硬)③ **人數未滿**(已排 < 需求,軟/缺口)④ **超週工時**(本版時數 > `max_weekly_hours`,軟)。`Publishable = 無硬違反`。
- `slotKey(weekday,hour)=weekday*100+hour`(hour 0–23,不碰撞)。技能檢查在無技能維度資料前**延後**。

### `autofill.go` — 一鍵建議排班(純函式)
- `SuggestSchedule(AutofillInput) []ScheduleAssignment`:**瓶頸優先貪婪 + 計分**。
  1. 每格候選 = 標可上 + 沒跨店占用(**只用候選人 → 產出保證零硬衝突、可直接發布**)。
  2. **瓶頸優先**:候選最少的格先排(稀缺資源先分配)。
  3. 計分挑人:非常想上 > 可配合 > 本週時數少 > id 穩定;尊重 `max_weekly_hours`;排不滿就留缺口(不硬塞)。
- `autofill_test.go` 5 案:只排可上 / 偏好優先 / 尊重週上限 / 不跨店雙排 / 瓶頸優先。

### handlers — HTTP 介面
- `handler.go`:`RegisterRoutes` 一次掛所有路由;**`writeDBError`** 把 Postgres 錯誤碼翻友善 4xx(`ErrNoRows`→404、`22P02` 壞 UUID→400、`23503` FK→400、`23514` CHECK→400,其餘 500)。各 handler 模式:`DecodeJSON` → `TrimSpace`+必填檢查 → 呼叫 repo → `JSON` / `writeDBError`;空清單回 `[]`。
- `handler_availability.go`:`createAccessLink`(店長發連結,只吃 `employee_id`)、`getMe`(員工開連結 → 可填門市清單)、`getAvailability` / `putAvailability`(需 `store_id`,走 `ResolveTokenForStore`)。token 無效一律 **401**。
- `handler_settings.go`:`getStoreHours`/`putStoreHours`、`getRequirements`/`putRequirements`。
- `handler_coverage.go`:`getCoverage` 把營業時段 × 7 天展開成逐小時格,算 `available = want+ok`、`gap = required - available`。
- `handler_schedule.go`:排班與兩階段的全部端點 —— `getSchedule`(回 draft + 驗證 + 最近發布版 + 確認狀態)、`putAssignments`、`autofillSchedule`、`publishSchedule`(硬違反→**409** 帶 validation;成功則 seed 確認 + 設截止)、`lockSchedule`(無 published→400)、`exportSchedule`(CSV,UTF-8 BOM,連續時段併區塊);員工端 `getMySchedule`、`confirmMySchedule`(已鎖定→擋)、`postMyIssue`(=回絕,已鎖定→擋)。

---

## 4. `db/` — schema 與 migration

### `embed.go`
`//go:embed migrations/*.sql` 把 SQL 檔包進 binary(`var MigrationsFS embed.FS`),`migrate.go` 用它套用 → **單一 binary 自帶 schema**,部署不必另外送 SQL。

### migration 序列(goose `-- +goose Up/Down`,append-only;啟動時自動套到最新)
- **`0001_init.sql`** — 初始 8 張表:租戶層 `organizations`/`stores`;人員層 `employees`、`employee_skills`、`employee_store_memberships`、`employee_access_tokens`(只存 `token_hash`);需求層 `shift_templates`;供給層 `availability_slots`(含 `preference_level`)。
- **`0002_access_token_store.sql`** — token 暫時加 `store_id`(Step 5 用)。
- **`0003_employee_link_membership.sql`** — 改一員工一連結:token **去 `store_id`、改綁員工**;新增 `availability_submissions`(提交標記,PK=員工+門市);既有員工 backfill 加入其組織所有門市。
- **`0004_hourly_availability.sql`** — 改逐小時:`stores` 加 `open_hour`/`close_hour`(預設 9/22);新表 `staffing_requirements`(weekday × hour × headcount);`availability_slots` **重建**為 `(employee, store, weekday, hour, preference 1/2)` 只存正向;**`DROP TABLE shift_templates`**(固定 4 班別淘汰)。
- **`0005_scheduling.sql`** — 排班:`employees` 加 `max_weekly_hours`(預設 40);`schedule_versions`(status draft/published + **partial unique** 保證每店至多一 draft);`shift_assignments`(version × employee × weekday × hour);`assignment_issues`。
- **`0006_two_phase.sql`** — 兩階段:`schedule_versions.status` CHECK 加 `locked`、加 `confirm_deadline`;新表 `shift_confirmations`(version × employee × status pending/confirmed/declined + reason)。

### 目前有效 schema(ER 摘要)
```
organizations 1─* stores
organizations 1─* employees 1─* employee_access_tokens         (token 綁員工, 只存 hash)
employees *─* stores            (employee_store_memberships)
employees ──  availability_submissions  (員工 × 店:提交標記)
stores 1─* staffing_requirements        (weekday × hour × headcount)        ← 需求(坑)
availability_slots: employee × store × weekday × hour × preference(1/2)     ← 供給(只存正向)
stores 1─* schedule_versions 1─* shift_assignments  (employee × weekday × hour)
schedule_versions 1─* shift_confirmations / assignment_issues
```
> 註:`shift_templates`、舊版 `availability_slots(preference 0–2)` 已被 0004 取代;migration 檔保留是歷史紀錄,**目前執行的是上述形狀**。

---

## 5. `web/` — 前端(CSR,同源)

> 依使用者分子目錄;資源統一由 `/static/` 服務,入口 `/`(管理台)、`/a/{token}`(填班)、`/s/{token}`(看班/確認)。

### `embed.go`
`//go:embed admin staff style.css dragGrid.js` 把整個前端包進 binary。

### `dragGrid.js`(共用元件)
when2meet 式**拖曳塗選**網格:Pointer Events + `elementFromPoint` 實作拖曳塗格,`touch-action:none` 防手機誤捲動,支援點表頭整欄 / 整列套用筆刷。**員工填可上時段**與**老闆設逐小時需求**共用同一元件。

### `admin/`(店長,入口 `/`)
- `index.html`:6 區 —— ①組織 ②門市 ③員工 ④營業時段 + 逐小時需求 ⑤缺口(需求 vs 可上)⑥排班(建議 → 微調 → 發布)。
- `app.js`:
  - `api(method, path, body)` fetch 封裝(把 `{"error"}` 轉例外;4xx 把 body 掛到例外上,讓 publish 409 能讀 `validation`)。
  - **狀態持久化**:選定的 org/store 存 `localStorage`(只記指標,真資料回 DB),重整後自動還原;切回分頁(window focus)自動重載缺口。
  - CRUD + membership:建組織/門市/員工、員工「門市」鈕調 membership、「發填班連結」(產生並自動複製)。
  - 第 4 區:營業時段 + 用筆刷在拖曳網格上「塗幾人」設逐小時需求。
  - 第 5 區:缺口 heatmap 畫**完整營業時段 × 7 天**網格;有需求的格上色(綠/黃/紅),只有供給沒需求的格顯示藍色供給數。
  - 第 6 區:排班格(選員工 → 塗該員工班);「🪄 一鍵建議排班」、「儲存」、「發布」、「🔒 鎖定」、「匯出 CSV」;`renderValidation`(硬/軟/缺口)、`renderConfirmations`(每位 confirmed/declined/pending + 倒數)、`renderIssues`。

### `staff/`(員工)
- `availability.{html,js}`(入口 `/a/{token}`):選門市(membership 內)→ 拖曳塗選填逐小時可上(筆刷:非常想上 / 可配合 / 清除)→ 送出只帶正向格。常駐錯誤區處理壞 token / 載入失敗(不消失,而非 toast 後留白)。
- `schedule.{html,js}`(入口 `/s/{token}`):看自己在某店「已發布」班表;`renderConfirmBar`(✅ 接受整週 + 狀態 + 截止倒數);點格 `flagIssue`(= 回絕,已鎖定則禁用)。

### `style.css`
共用樣式 + `.paint-grid` / `touch-action:none`(拖曳網格)+ `.brush` / `.req-set`(筆刷與需求)+ 缺口與排班的狀態色 + 確認面板樣式。觸控目標 `min-height:44px`。

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
  建門市 ── POST /api/stores            → 用 DB 預設營業時段(09–22)
  建員工 ── POST /api/employees         → 同 tx 加入該組織所有門市 membership
  設時段 ── PUT  /api/store-hours       → stores.open_hour/close_hour
  設需求 ── PUT  /api/requirements      → staffing_requirements(逐小時要幾人)
  發連結 ── POST /api/access-links      → 回 /a/<token>(DB 只存 hash,綁員工)
        │
員工(/a/<token>)
  開頁   ── GET  /api/me                → 我能填哪些門市(membership)
  填存   ── PUT  /api/availability      → 整批覆寫逐小時可上(只存正向)+ 標記已提交
        │
店長(管理台 第5、6區)
  看缺口 ── GET  /api/coverage          → 小時 × 星期 需求vs可上 + 未填名單
  建草稿 ── GET  /api/schedule          → 取/開 draft(可複製自最近發布版)
  建議   ── POST /api/schedule/autofill → 瓶頸優先 + 計分產草稿(零硬衝突)
  微調   ── PUT  /api/schedule/assignments
  發布   ── POST /api/schedule/publish  → 硬違反 409;否則 published + seed 確認 + 設 24h 截止
        │
員工(/s/<token>)
  看班   ── GET  /api/my-schedule       → 我的班 + my_status + 截止
  確認   ── POST /api/my-schedule/confirm   → 接受整週(confirmed)
  回絕   ── POST /api/my-schedule/issues    → 標問題格(declined + 理由)
        │
店長(管理台 第6區)
  看確認 ── GET  /api/schedule          → 每位 confirmed/declined/pending + 倒數
  鎖定   ── POST /api/schedule/lock     → published → locked(定案;之後員工回應被擋)
  匯出   ── GET  /api/schedule/export   → CSV(UTF-8 BOM)
```
