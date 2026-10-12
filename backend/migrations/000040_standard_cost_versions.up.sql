-- 標準成本版本與生效日(D99):同一料品可有多筆不同生效日的標準成本,差異報表依完工日取當時有效的一筆
-- 既有資料的生效日為 2000-01-01(= 一直有效)
ALTER TABLE item_standard_costs ADD COLUMN effective_from DATE NOT NULL DEFAULT DATE '2000-01-01';
ALTER TABLE item_standard_costs DROP CONSTRAINT item_standard_costs_pkey;
ALTER TABLE item_standard_costs ADD PRIMARY KEY (item_id, effective_from);
