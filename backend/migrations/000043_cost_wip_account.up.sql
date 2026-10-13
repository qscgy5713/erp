-- 在製品會計科目(D102):月結時把在製品倉的材料價值從存貨科目轉到「在製品」科目
ALTER TABLE cost_closings ADD COLUMN wip_value NUMERIC(18,4) NOT NULL DEFAULT 0; -- 期末在製品金額(已含在 inventory_value 內)

INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '1142', '在製品', 'asset' FROM companies c ON CONFLICT (company_id, code) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'cost.wip', a.id FROM accounts a WHERE a.code = '1142'
ON CONFLICT (company_id, map_key) DO NOTHING;
