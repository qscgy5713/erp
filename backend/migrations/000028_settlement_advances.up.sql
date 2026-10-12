-- 預收 / 預付款(D82):收付款單可多收 / 多付一筆預收(預付)款,也可拿既有的預收(預付)款抵用
ALTER TABLE settlements
    ADD COLUMN advance_amount NUMERIC(18,4) NOT NULL DEFAULT 0, -- 本單新增的預收 / 預付
    ADD COLUMN advance_used   NUMERIC(18,4) NOT NULL DEFAULT 0, -- 本單抵用的既有預收 / 預付
    ADD CONSTRAINT settlements_advance_check CHECK (advance_amount >= 0 AND advance_used >= 0);

INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'settle.advance_received', a.id FROM accounts a WHERE a.code = '2121'
ON CONFLICT (company_id, map_key) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'settle.advance_paid', a.id FROM accounts a WHERE a.code = '1151'
ON CONFLICT (company_id, map_key) DO NOTHING;
