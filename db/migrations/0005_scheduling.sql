-- +goose Up
-- v2:排班指派 + 發布 + 雙階段標記。設計依據見 docs/design.md 決策 3/4/5 + 決策 8(v2 接法)。
--
-- 粒度決策(2026-06-02 拍板):**逐小時指派**(沿用 v1.5 的「小時 × 星期」格子),
-- 不另設 scheduled_shift 表 —— 需求「坑」就是 staffing_requirements 的 (store,weekday,hour) 列,
-- 指派就是 (version, employee, weekday, hour) 一格一筆。
-- 週模型決策:**循環週 + 版本快照** —— 一張範本週班表,draft 編輯、發布凍結成 published 快照;
-- 改已發布就開新 draft(複製自最近 published),舊版保留(對齊 design §3.7 immutability)。

-- 員工週工時上限(超過 → 軟警告,不擋)。沒有就用預設 40。
ALTER TABLE employees ADD COLUMN IF NOT EXISTS max_weekly_hours smallint NOT NULL DEFAULT 40
    CHECK (max_weekly_hours >= 0);

-- 班表版本:一店多版本。status=draft 編輯中、published 已凍結。
CREATE TABLE schedule_versions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id     uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
CREATE INDEX idx_versions_store ON schedule_versions(store_id);
-- 一間店同時最多一個 draft(避免並發產生多份草稿)。
CREATE UNIQUE INDEX uniq_draft_per_store ON schedule_versions(store_id) WHERE status = 'draft';

-- 逐小時指派:某版本裡,某員工被排在 (weekday, hour) 這一格。
-- PK 防同版本同員工同格重複;跨店同時段「雙排」由 Rule Engine 查其他店已發布版本判斷。
CREATE TABLE shift_assignments (
    version_id  uuid NOT NULL REFERENCES schedule_versions(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    weekday     smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    hour        smallint NOT NULL CHECK (hour BETWEEN 0 AND 23),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (version_id, employee_id, weekday, hour)
);
CREATE INDEX idx_assignments_version ON shift_assignments(version_id);
CREATE INDEX idx_assignments_employee ON shift_assignments(employee_id);

-- 簡化雙階段:員工在「已發布」班表上標記「我這格有問題」(決策 5 簡化版)。
CREATE TABLE assignment_issues (
    version_id  uuid NOT NULL REFERENCES schedule_versions(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    weekday     smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    hour        smallint NOT NULL CHECK (hour BETWEEN 0 AND 23),
    note        text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (version_id, employee_id, weekday, hour)
);

-- +goose Down
DROP TABLE IF EXISTS assignment_issues;
DROP TABLE IF EXISTS shift_assignments;
DROP TABLE IF EXISTS schedule_versions;
ALTER TABLE employees DROP COLUMN IF EXISTS max_weekly_hours;
