DROP TABLE checks;
ALTER TABLE settlements DROP COLUMN check_due_date;
DELETE FROM account_mappings WHERE map_key IN ('settle.notes_receivable', 'settle.notes_payable');
