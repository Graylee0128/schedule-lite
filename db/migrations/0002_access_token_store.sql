-- +goose Up
-- Step 5:magic-link 要綁定「哪一間店」,員工點開連結才知道要填哪些班別。
-- employee_access_tokens 原本只綁 employee,這裡補上 store_id。
ALTER TABLE employee_access_tokens
    ADD COLUMN store_id uuid REFERENCES stores(id) ON DELETE CASCADE;
CREATE INDEX idx_tokens_store ON employee_access_tokens(store_id);

-- +goose Down
DROP INDEX IF EXISTS idx_tokens_store;
ALTER TABLE employee_access_tokens DROP COLUMN IF EXISTS store_id;
