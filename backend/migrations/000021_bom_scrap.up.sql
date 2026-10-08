-- BOM 材料損耗率(%):展開工單領料時,用量 × (1 + 損耗率 ÷ 100)
ALTER TABLE bom_lines ADD COLUMN scrap_pct NUMERIC(5,2) NOT NULL DEFAULT 0;
ALTER TABLE bom_lines ADD CONSTRAINT bom_lines_scrap_check CHECK (scrap_pct >= 0 AND scrap_pct < 100);
