-- +goose Up
-- v1.5 階段 B:availability / 需求 改「逐小時」,並把固定 4 班別淘汰。
-- 設計依據見 docs/design.md 決策 8.2~8.4、docs/plan.md v1.5 階段 B。

-- 1) 店加營業時段(單一窗,套用 7 天)。需求人數為 0 的小時等同當天沒排班需求,
--    格子只顯示 [open_hour, close_hour) 內的小時。週末/不同日只要把需求設 0 即可。
ALTER TABLE stores ADD COLUMN IF NOT EXISTS open_hour  smallint NOT NULL DEFAULT 9  CHECK (open_hour  BETWEEN 0 AND 24);
ALTER TABLE stores ADD COLUMN IF NOT EXISTS close_hour smallint NOT NULL DEFAULT 22 CHECK (close_hour BETWEEN 0 AND 24);

-- 2) 逐小時需求人數(只存 >0 的列;不存在 = 該 (日,時) 不需要人)。
CREATE TABLE staffing_requirements (
    store_id  uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    weekday   smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    hour      smallint NOT NULL CHECK (hour BETWEEN 0 AND 23),
    headcount smallint NOT NULL CHECK (headcount >= 0),
    PRIMARY KEY (store_id, weekday, hour)
);

-- 3) availability 改逐小時、只存「能上」(preference 1/2);未塗 = 絕對不行(預設,不落 DB)。
--    淘汰 shift_template 綁定與 specific_date(單週覆寫留到之後再說)。舊資料是測試資料,直接重建。
DROP TABLE IF EXISTS availability_slots;
CREATE TABLE availability_slots (
    employee_id      uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    store_id         uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    weekday          smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    hour             smallint NOT NULL CHECK (hour BETWEEN 0 AND 23),
    preference_level smallint NOT NULL CHECK (preference_level BETWEEN 1 AND 2),
    PRIMARY KEY (employee_id, store_id, weekday, hour)
);
CREATE INDEX idx_availability_store ON availability_slots(store_id);

-- 4) 班別模板淘汰:逐小時需求(staffing_requirements)取代固定 4 班。
DROP TABLE IF EXISTS shift_templates;

-- +goose Down
-- 回退到 0003 結束狀態:重建 shift_templates 與「4 班 + 三元」的 availability_slots。
DROP TABLE IF EXISTS availability_slots;
DROP TABLE IF EXISTS staffing_requirements;
ALTER TABLE stores DROP COLUMN IF EXISTS close_hour;
ALTER TABLE stores DROP COLUMN IF EXISTS open_hour;

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

CREATE TABLE availability_slots (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id      uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    store_id         uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    shift_template_id uuid REFERENCES shift_templates(id) ON DELETE CASCADE,
    weekday          smallint CHECK (weekday BETWEEN 0 AND 6),
    specific_date    date,
    preference_level smallint NOT NULL DEFAULT 1 CHECK (preference_level BETWEEN 0 AND 2),
    created_at       timestamptz NOT NULL DEFAULT now(),
    CHECK ((weekday IS NOT NULL) <> (specific_date IS NOT NULL))
);
CREATE INDEX idx_availability_employee ON availability_slots(employee_id);
CREATE INDEX idx_availability_store ON availability_slots(store_id);
