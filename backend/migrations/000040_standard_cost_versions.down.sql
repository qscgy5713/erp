-- 還原為每個料品一筆:保留生效日最晚的一筆
DELETE FROM item_standard_costs s USING item_standard_costs k
 WHERE s.item_id = k.item_id AND s.effective_from < k.effective_from;
ALTER TABLE item_standard_costs DROP CONSTRAINT item_standard_costs_pkey;
ALTER TABLE item_standard_costs ADD PRIMARY KEY (item_id);
ALTER TABLE item_standard_costs DROP COLUMN effective_from;
