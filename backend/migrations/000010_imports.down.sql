ALTER TABLE accounts_payable DROP COLUMN IF EXISTS import_batch_id;
ALTER TABLE accounts_receivable DROP COLUMN IF EXISTS import_batch_id;
DROP SEQUENCE IF EXISTS opening_seq;
DROP TABLE IF EXISTS import_batches;
