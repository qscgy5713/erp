-- 還原為一個成品一份 BOM:每個成品只保留生效日最晚的一份
DELETE FROM boms b USING boms n
WHERE b.company_id = n.company_id AND b.item_id = n.item_id AND b.effective_from < n.effective_from;
ALTER TABLE boms DROP CONSTRAINT boms_company_item_effective_key;
ALTER TABLE boms ADD CONSTRAINT boms_company_item_key UNIQUE (company_id, item_id);
ALTER TABLE boms DROP COLUMN effective_from;
