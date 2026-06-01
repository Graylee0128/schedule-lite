-- +goose Up
-- v1 基礎 schema:租戶 / 人員 / 需求 / 供給 四層。
-- 排班記錄層(schedule_versions / scheduled_shifts / shift_assignments)與
-- 考勤層(attendance_*)因為有前向相依,留到後續 migration 再建。
-- 設計依據見 docs/design.md §3.6 / §3.7。

-- 租戶 / 組織層 -------------------------------------------------------------

-- 組織 / 品牌:多租戶的根。timezone 決定所有牆鐘時間的解讀。
CREATE TABLE organizations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    timezone   text NOT NULL DEFAULT 'Asia/Taipei',
    week_start smallint NOT NULL DEFAULT 1, -- 1=週一起算,0=週日
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 門市 / 分店。
CREATE TABLE stores (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_stores_org ON stores(organization_id);

-- 人員層 -------------------------------------------------------------------

-- 員工:綁在最高層 org,不死綁單店(跨店支援靠 memberships)。
CREATE TABLE employees (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL,
    phone           text,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_employees_org ON employees(organization_id);

-- 員工技能(會結帳 / 開店 / 關店…),影響排班軟性檢查。
CREATE TABLE employee_skills (
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    skill       text NOT NULL,
    PRIMARY KEY (employee_id, skill)
);

-- 員工 ↔ 門市:多對多。role 放這層(同一人在不同店角色可不同)。
CREATE TABLE employee_store_memberships (
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    store_id    uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    role        text NOT NULL DEFAULT 'employee'
                CHECK (role IN ('owner', 'manager', 'employee')),
    is_active   boolean NOT NULL DEFAULT true,
    PRIMARY KEY (employee_id, store_id)
);
CREATE INDEX idx_memberships_store ON employee_store_memberships(store_id);

-- 員工 magic-link token:只存 hash,可設過期、可撤銷。
CREATE TABLE employee_access_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    token_hash  text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,
    revoked_at  timestamptz
);
CREATE INDEX idx_tokens_employee ON employee_access_tokens(employee_id);

-- 需求 / 規則層 -------------------------------------------------------------

-- 班別模板:這間店「需要」什麼班、幾人、什麼技能。起訖用店本地牆鐘時間。
CREATE TABLE shift_templates (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id           uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    name               text NOT NULL,
    start_local        time NOT NULL,
    end_local          time NOT NULL,
    required_headcount int  NOT NULL DEFAULT 1 CHECK (required_headcount >= 0),
    required_skills    jsonb NOT NULL DEFAULT '[]',
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_shift_templates_store ON shift_templates(store_id);

-- 供給層 -------------------------------------------------------------------

-- 員工可上時段(positive availability)。weekday=循環規則;
-- specific_date=單週覆寫(優先級高)。preference_level 三元:
-- 0=絕對不行 / 1=可配合 / 2=非常想上。
CREATE TABLE availability_slots (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id      uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    store_id         uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    shift_template_id uuid REFERENCES shift_templates(id) ON DELETE CASCADE,
    weekday          smallint CHECK (weekday BETWEEN 0 AND 6),
    specific_date    date,
    preference_level smallint NOT NULL DEFAULT 1 CHECK (preference_level BETWEEN 0 AND 2),
    created_at       timestamptz NOT NULL DEFAULT now(),
    -- weekday(循環)與 specific_date(覆寫)二擇一
    CHECK ((weekday IS NOT NULL) <> (specific_date IS NOT NULL))
);
CREATE INDEX idx_availability_employee ON availability_slots(employee_id);
CREATE INDEX idx_availability_store ON availability_slots(store_id);

-- +goose Down
DROP TABLE IF EXISTS availability_slots;
DROP TABLE IF EXISTS shift_templates;
DROP TABLE IF EXISTS employee_access_tokens;
DROP TABLE IF EXISTS employee_store_memberships;
DROP TABLE IF EXISTS employee_skills;
DROP TABLE IF EXISTS employees;
DROP TABLE IF EXISTS stores;
DROP TABLE IF EXISTS organizations;
