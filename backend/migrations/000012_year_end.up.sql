-- M8 財務報表與年度結帳:年結傳票結轉到「保留盈餘」的拋轉規則(D59)
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'year.retained', a.id FROM accounts a WHERE a.code = '3201'
ON CONFLICT (company_id, map_key) DO NOTHING;
