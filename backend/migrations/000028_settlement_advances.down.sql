DELETE FROM account_mappings WHERE map_key IN ('settle.advance_received', 'settle.advance_paid');
ALTER TABLE settlements DROP COLUMN advance_amount, DROP COLUMN advance_used;
