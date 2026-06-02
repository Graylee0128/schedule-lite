-- +goose Up
-- v3 階段 B:完整兩階段提交。發布後員工確認/回絕,全員確認後老闆鎖定。
-- 採「軟截止 + 老闆手動鎖」:confirm_deadline 只顯示倒數,不靠背景排程強制;老闆自己按鎖定。
-- 設計依據見 docs/design.md 決策 5。

-- 版本狀態多一個 locked(draft 編輯 → published 確認中 → locked 定案)。
ALTER TABLE schedule_versions DROP CONSTRAINT IF EXISTS schedule_versions_status_check;
ALTER TABLE schedule_versions ADD CONSTRAINT schedule_versions_status_check
    CHECK (status IN ('draft', 'published', 'locked'));
ALTER TABLE schedule_versions ADD COLUMN IF NOT EXISTS confirm_deadline timestamptz;

-- 員工對某版本的確認狀態(發布時對該版本有班的員工各 seed 一筆 pending)。
-- declined 沿用「標問題」流程(回絕 = 對某格標問題 + 把此狀態設 declined + 理由)。
CREATE TABLE shift_confirmations (
    version_id   uuid NOT NULL REFERENCES schedule_versions(id) ON DELETE CASCADE,
    employee_id  uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'declined')),
    reason       text NOT NULL DEFAULT '',
    responded_at timestamptz,
    PRIMARY KEY (version_id, employee_id)
);

-- +goose Down
DROP TABLE IF EXISTS shift_confirmations;
ALTER TABLE schedule_versions DROP COLUMN IF EXISTS confirm_deadline;
ALTER TABLE schedule_versions DROP CONSTRAINT IF EXISTS schedule_versions_status_check;
ALTER TABLE schedule_versions ADD CONSTRAINT schedule_versions_status_check
    CHECK (status IN ('draft', 'published'));
