-- +goose Up
-- v1.5 階段 A:token 改綁員工(去 store)、員工填班提交標記、既有員工補 membership。
-- 設計依據見 docs/design.md 決策 8、docs/plan.md v1.5 階段 A。

-- 1) token 去 store 綁定(回退 0002):一人一條長期連結,不再綁單店;
--    員工開連結後自己選 membership 內的門市。
ALTER TABLE employee_access_tokens DROP COLUMN IF EXISTS store_id;

-- 2) 提交標記:記某員工對某店「有沒有回應過」,
--    用來分辨「沒提交的絕對不行」vs「提交了的絕對不行」(否則「誰沒填」會失真)。
CREATE TABLE availability_submissions (
    employee_id  uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    store_id     uuid NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    submitted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (employee_id, store_id)
);

-- 3) 既有員工補 membership(預設加入其組織所有門市),讓舊資料也能多店填班。
--    新員工的 membership 由應用層在建檔時 seed(見 repo.CreateEmployee)。
INSERT INTO employee_store_memberships (employee_id, store_id)
SELECT e.id, s.id
FROM employees e
JOIN stores s ON s.organization_id = e.organization_id
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS availability_submissions;
ALTER TABLE employee_access_tokens ADD COLUMN store_id uuid REFERENCES stores(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_tokens_store ON employee_access_tokens(store_id);
