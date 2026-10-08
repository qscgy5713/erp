-- BOM 版本:同一成品可有多份 BOM,各自有生效日;工單依工單日期選用「已生效且啟用」中生效日最晚的一份
ALTER TABLE boms ADD COLUMN effective_from DATE NOT NULL DEFAULT DATE '2000-01-01';
ALTER TABLE boms DROP CONSTRAINT boms_company_item_key;
ALTER TABLE boms ADD CONSTRAINT boms_company_item_effective_key UNIQUE (company_id, item_id, effective_from);
