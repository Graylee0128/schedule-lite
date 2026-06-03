# schedule-lite

針對小型店家 / 加盟店 / 連鎖門市的 **availability-first 智慧排班輔助系統**。

> 設計與開發文件(構想、架構、資料模型、開發紀錄)維護在內部 `docs/`,未隨此 repo 發佈。

## 功能

- **組織 / 門市 / 員工管理**:多租戶結構;員工 ↔ 門市多對多歸屬,一員工一條連結、可跨店填班。
- **逐小時營業時段 + 需求人數**:老闆設每店營業時段,並用塗選網格設「哪個小時要幾人」。
- **when2meet 拖曳填班**:員工免註冊點開連結,拖曳塗選每週逐小時可上時段(兩級偏好,未塗=不能上);填班與設需求共用同一塗選網格。
- **缺口分析 heatmap**:小時 × 星期一頁看「需求 vs 可上人數、誰還沒填」。
- **一鍵建議排班**:用「瓶頸優先貪婪 + 計分」把可上時段排成建議草稿——只排員工標可上的格,保證零硬衝突,排不滿留缺口,老闆再微調。
- **衝突檢查(Rule Engine)**:不可用時段、跨店同時段雙排為硬衝突(擋發布);人數未滿、超週工時為軟警告。
- **兩階段提交**:發布 → 員工「接受整週 / 回報問題(=回絕)」→ 老闆看確認狀態 + 倒數 → 手動鎖定定案(`draft → published → locked`)。
- **CSV 匯出**:連續小時自動併成班段(員工 / 星期 / 起迄 / 時數),Excel 可開。

技術面:單一 Go binary(標準庫 `net/http` + 同源 embed 前端)後接 PostgreSQL,`docker compose` 一鍵起;schema migration 隨 binary 自帶;附 `/healthz` `/readyz` 探針。

> ⚠️ 首次 build 前需要 `go.sum`:在有 Go 的機器上先跑 `go mod tidy`,或直接用下方「方式 A」讓容器自動產生。

## 前置(一次性)

本機需要:

- **Go 1.22+** — <https://go.dev/dl/>
- **Docker Desktop**(含 compose)— <https://www.docker.com/products/docker-desktop/>

驗證:`go version`、`docker version` 都有輸出即可。

## 本機跑起來

### 方式 A:零設定一鍵(全新機器,最推薦)

只要有 **Docker**(連 Go 都不用裝),一行把整包抓下來、build、起服務、驗探針:

```bash
curl -fsSL https://raw.githubusercontent.com/Graylee0128/schedule-lite/main/get.sh | bash
```

- 預設裝到目前目錄下的 `schedule-lite/`,服務起在 <http://localhost:8080>。
- `go.sum` 用容器自動產生;埠衝突(8080/5432)會自動換埠;**可重複執行**(中途失敗再跑一次不會壞)。
- 收掉:`cd schedule-lite && bash scripts/teardown.sh`。

### 方式 B:已 clone,用部署腳本

```bash
go mod tidy                # 第一次:解析 pgx/goose 產生 go.sum(沒裝 Go 就用方式 A)
bash scripts/deploy.sh     # build + 起 compose + 驗證探針
bash scripts/teardown.sh   # 收掉(加 -v 連 DB 資料一起清)
```

### 方式 C:手動 compose(app + postgres)

```bash
docker compose up --build
# 另開視窗:
curl http://localhost:8080/healthz   # → {"status":"ok"}
curl http://localhost:8080/readyz    # → {"status":"ready"}(DB 通才會 ready)
```

### 方式 D:host 上 go run(postgres 仍用 compose 起)

```bash
docker compose up -d db              # 只起 postgres
go run ./cmd/server                  # app 連 localhost:5432
```

## 管理台(前端)

部署後直接開瀏覽器(同源,前端 embed 在 binary 裡):

```text
http://localhost:8080/          # 或 http://<主機 IP>:8080/
```

依序操作:選/建組織 → 建門市 → 選門市 → 建員工 → 設**營業時段 + 逐小時需求人數**(拖曳塗選網格)→ 看**逐小時缺口 heatmap**(需求 vs 可上、未填名單)。
選定的組織/門市會記在 localStorage,**重整頁面自動還原**(真資料仍回 DB)。全部透過 `fetch` 打下面的 JSON API。

### 員工填班(magic-link,一員工一連結、可跨店)

1. 管理台員工列點「發填班連結」→ 取得 `http://<host>:8080/a/<token>`(**綁員工、不綁門市**,自動複製);需要時用「門市」鈕調整他能填哪些店(新員工預設全店)。
2. 把連結傳給員工;員工**免註冊**點開 → **先選門市**(只列他被指派的店,單店自動進)→ 在「時段 × 星期」網格用 when2meet 式**拖曳塗選**意願(未塗=不能上),按儲存。
3. token 只存 SHA-256 hash;一人一條長期連結,可跨其門市分別填班。重開會帶出先前填的(整批覆寫),並記「已提交」標記(缺口的「未填名單」據此判斷)。

### 排班 → 發布 → 員工確認 → 鎖定

1. 管理台第 6 區「排班」:**最快是按「🪄 一鍵建議排班」**——系統用「瓶頸優先貪婪 + 計分」把員工填的可上時段排成建議草稿(只排他們標可上的格,**保證零硬衝突**,排不滿留缺口),老闆再微調。也可手動:選員工 → 「指派/取消」筆刷 → 拖曳排班。格內數字 = 已排/需求;✓ = 排給這位;**紅框** = 他沒標可上(排了就是硬衝突)。存檔即時跑 **Rule Engine**(不可用/跨店雙排=🔴硬;缺口/超週工時=🟡軟)。
2. **發布**:有硬衝突擋下(409 標紅);排除後發布 → 凍結快照,並對有班的員工發出**確認**(24h 軟截止)。要再改就自動開新草稿(複製自已發布/鎖定版,舊版留存)。
3. **員工確認**:員工開 `http://<host>:8080/s/<token>` → 選門市看自己**已發布**班表 →「✅ 接受整週」或點 ✓ 格子**回報問題(= 回絕,附理由)**。老闆第 6 區看每位 ✅/⚠️/⏳ + 倒數。
4. **鎖定**:老闆按「🔒 鎖定」→ 版本 `locked` 定案(未全確認會提醒;店長說了算)。鎖定後員工不可再回應;要改需重新發布新版。
5. **匯出 CSV**:連續小時自動併成班段(員工/星期/起迄/時數),Excel 可開。

## API

JSON CRUD,用 curl 走一遍垂直切片:建組織 → 建門市/員工 → 列出。

```bash
# 1. 建組織(回傳含 id)
ORG=$(curl -sS -X POST localhost:8080/api/organizations \
  -H 'Content-Type: application/json' -d '{"name":"胖老爹"}')
echo "$ORG"
ORG_ID=$(echo "$ORG" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

# 2. 在該組織建門市與員工
curl -sS -X POST localhost:8080/api/stores \
  -H 'Content-Type: application/json' \
  -d "{\"organization_id\":\"$ORG_ID\",\"name\":\"板橋店\"}"
curl -sS -X POST localhost:8080/api/employees \
  -H 'Content-Type: application/json' \
  -d "{\"organization_id\":\"$ORG_ID\",\"name\":\"小明\",\"phone\":\"0912345678\"}"

# 3. 列出
curl -sS "localhost:8080/api/stores?organization_id=$ORG_ID"
curl -sS "localhost:8080/api/employees?organization_id=$ORG_ID"

# 4. 驗證輸入驗證:壞的 organization_id 應回 400
curl -sS -X POST localhost:8080/api/stores \
  -H 'Content-Type: application/json' -d '{"organization_id":"not-a-uuid","name":"x"}'
```

| 路由 | 說明 |
|---|---|
| `POST /api/organizations` | 建組織 `{name, timezone?}` |
| `GET /api/organizations` | 列所有組織(管理台用來選既有) |
| `POST /api/stores` | 建門市 `{organization_id, name}` |
| `GET /api/stores?organization_id=` | 列門市 |
| `POST /api/employees` | 建員工 `{organization_id, name, phone?}`(自動加入該組織所有門市) |
| `GET /api/employees?organization_id=` | 列員工 |
| `GET /api/memberships?employee_id=` | 列員工可填的門市 |
| `POST /api/memberships` | 加入門市 `{employee_id, store_id}` |
| `DELETE /api/memberships?employee_id=&store_id=` | 移出門市 |
| `GET /api/store-hours?store_id=` | 取營業時段 → `{open_hour, close_hour}` |
| `PUT /api/store-hours?store_id=` | 設營業時段 `{open_hour, close_hour}` |
| `GET /api/requirements?store_id=` | 列逐小時需求 `[{weekday, hour, headcount}]`(只回 headcount>0) |
| `PUT /api/requirements?store_id=` | 整批覆寫逐小時需求 `{requirements:[{weekday, hour, headcount}]}` |
| `POST /api/access-links` | 產生員工填班 magic-link `{employee_id}` → `{token, url}`(綁員工、不綁門市) |
| `GET /api/me?token=` | 員工開連結的初始資料(員工 + 可填門市清單) |
| `GET /api/availability?token=&store_id=` | 某門市的營業時段 + 員工已塗時段 `{open_hour, close_hour, slots:[{weekday, hour, preference_level}]}` |
| `PUT /api/availability?token=&store_id=` | 整批覆寫該門市可上時段 `{slots:[{weekday, hour, preference_level}]}`(只存正向 1/2、並記提交標記) |
| `GET /api/coverage?store_id=` | 逐小時缺口分析(每格 需求 vs 可上、未填名單依 membership + 提交標記) |
| `GET /api/schedule?store_id=` | 取/建排班 draft + 候選員工 + 需求 + 指派 + 驗證 + 已發布版的員工問題 |
| `PUT /api/schedule/assignments?store_id=` | 整批覆寫某員工在 draft 的指派 `{employee_id, slots:[{weekday,hour}]}` → `{assignments, validation}` |
| `POST /api/schedule/autofill?store_id=` | 一鍵建議排班(預排):整批覆寫 draft → `{suggested, assignments, validation}`(零硬衝突) |
| `POST /api/schedule/publish?store_id=` | 發布 draft;有硬衝突回 409 + validation;成功 seed 員工確認 + 24h 截止 |
| `POST /api/schedule/lock?store_id=` | 老闆鎖定最近發布版(`locked` 定案);無已發布回 400 |
| `GET /api/schedule/export?store_id=` | 匯出 CSV(連續小時併班段;優先最近發布版) |
| `GET /api/employee-availability?store_id=&employee_id=` | 某員工在該店可上格(排班底圖) |
| `GET /api/my-schedule?token=&store_id=` | 員工看自己在該店已發布班表 + 確認狀態 + 截止 + 是否鎖定 |
| `POST /api/my-schedule/confirm?token=&store_id=` | 員工「接受整週」→ confirmed(鎖定後擋) |
| `POST /api/my-schedule/issues?token=&store_id=` | 員工標記某格有問題 `{weekday, hour, note}`(= 回絕,設 declined + 理由) |

> 註:`/api/*`(店長端)目前尚無身分驗證(任何人可呼叫),為已知限制。
> 員工端 `/api/me` `/api/availability` 靠 magic-link 的 **token** 認證(token 只存 SHA-256 hash);token **綁員工**,可跨其 membership 的多店填班。

### 營業時段 + 逐小時需求

老闆設**營業時段 + 逐小時需求人數**;建門市時用 DB 預設時段(09–22),之後在管理台調整。

```bash
# 用上面的 $ORG_ID 建門市,拿 store_id
STORE=$(curl -sS -X POST localhost:8080/api/stores \
  -H 'Content-Type: application/json' \
  -d "{\"organization_id\":\"$ORG_ID\",\"name\":\"板橋店\"}")
STORE_ID=$(echo "$STORE" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

# 看/改營業時段
curl -sS "localhost:8080/api/store-hours?store_id=$STORE_ID"            # → {"open_hour":9,"close_hour":22}
curl -sS -X PUT "localhost:8080/api/store-hours?store_id=$STORE_ID" \
  -H 'Content-Type: application/json' -d '{"open_hour":8,"close_hour":24}'

# 設逐小時需求(週一 12 點要 2 人),整批覆寫
curl -sS -X PUT "localhost:8080/api/requirements?store_id=$STORE_ID" \
  -H 'Content-Type: application/json' \
  -d '{"requirements":[{"weekday":1,"hour":12,"headcount":2}]}'
curl -sS "localhost:8080/api/requirements?store_id=$STORE_ID"

# 壞時段(開店晚於關店)→ 400
curl -sS -X PUT "localhost:8080/api/store-hours?store_id=$STORE_ID" \
  -H 'Content-Type: application/json' -d '{"open_hour":22,"close_hour":8}'
```

## 查資料庫(Adminer / DBeaver)

PostgreSQL 沒有 phpMyAdmin(那是 MySQL 的)。這裡提供兩種等價方案:

### 方案 1:Adminer(內建在 compose,web GUI,推薦)

用 `tools` profile 關著——預設不啟動、不進 prod;要查資料時才開:

```bash
docker compose --profile tools up -d adminer   # 起 Adminer
# 瀏覽器開 http://<host>:8081
#   System   = PostgreSQL
#   Server   = db          (已預帶)
#   Username = schedule
#   Password = schedule
#   Database = schedule_lite
docker compose stop adminer                     # 查完關掉
```

> ⚠️ Adminer 是 DB 管理介面,**只開在內網/本機**,別曝露到公網或 prod。

### 方案 2:DBeaver(桌面,連已開放的 5432)

compose 已 `ports: 5432:5432`,桌面 DBeaver 直接連:

```text
Host: <主機 IP>   Port: 5432
DB: schedule_lite   User: schedule   Password: schedule
```

### 方案 3:psql(免裝,最快)

```bash
docker compose exec db psql -U schedule -d schedule_lite -c '\dt'   # 列表
docker compose exec db psql -U schedule -d schedule_lite            # 互動式
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
cmd/server/                 進入點(main + 連線 + migration + 路由 + 優雅關閉)
internal/
  platform/
    config/                 env 設定載入
    httpx/                  探針 + JSON 輔助
    pg/                     pgx 連線池 + goose migration 執行
  store/                    全部業務 domain(組織/門市/員工/membership/時段/需求/填班/缺口/排班/兩階段)
                            handler_*.go → repo_*.go 分層;rules.go / autofill.go 為純函式
db/
  embed.go                  把 migration 檔 embed 進 binary
  migrations/               SQL migration(goose 格式)
web/                        前端(CSR,依使用者分兩個子目錄)
  admin/                    管理台(店長):index.html / app.js,入口 /
  staff/                    員工頁:availability.*(填班 /a/{token})、schedule.*(看班/確認 /s/{token})
  dragGrid.js               when2meet 拖曳塗選共用元件
  style.css                 共用樣式(資源統一由 /static/ 服務)
  embed.go                  把前端 embed 進 binary,同源服務
scripts/
  deploy.sh                 build + 起 compose + 驗證探針
  teardown.sh               收掉 compose(-v 連資料清)
Dockerfile                  多階段 distroless
docker-compose.yml          app + postgres
```
