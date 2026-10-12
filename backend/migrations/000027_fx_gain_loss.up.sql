-- 外幣收付款的已實現匯兌損益(D81):收付款日匯率與帳款入帳匯率的差額
INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '4902', '兌換利益', 'revenue' FROM companies c ON CONFLICT (company_id, code) DO NOTHING;
INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '7102', '兌換損失', 'expense' FROM companies c ON CONFLICT (company_id, code) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'fx.gain', a.id FROM accounts a WHERE a.code = '4902'
ON CONFLICT (company_id, map_key) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'fx.loss', a.id FROM accounts a WHERE a.code = '7102'
ON CONFLICT (company_id, map_key) DO NOTHING;
