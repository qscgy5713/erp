-- name: CreateImportBatch :one
INSERT INTO import_batches (company_id, import_type, filename, row_count, summary, created_by)
VALUES (@company_id, @import_type, @filename, @row_count, @summary, @created_by)
RETURNING *;

-- name: ListImportBatches :many
SELECT b.*, u.name AS created_by_name FROM import_batches b LEFT JOIN users u ON u.id = b.created_by
WHERE b.company_id = @company_id ORDER BY b.id DESC LIMIT 50;

-- name: AllItemCodes :many
SELECT id, code, item_type, base_unit_id, lot_control FROM items WHERE company_id = @company_id;

-- name: AllCustomerCodes :many
SELECT id, code FROM customers WHERE company_id = @company_id;

-- name: AllSupplierCodes :many
SELECT id, code FROM suppliers WHERE company_id = @company_id;

-- name: AllBarcodes :many
SELECT barcode FROM items WHERE company_id = @company_id AND barcode IS NOT NULL;

-- name: AllAccountsForImport :many
SELECT id, code, name, is_active, is_postable FROM accounts WHERE company_id = @company_id;

-- name: NextOpeningSeq :one
SELECT nextval('opening_seq')::bigint;

-- name: HasOpeningBalanceVoucher :one
SELECT EXISTS (SELECT 1 FROM vouchers v WHERE v.company_id = @company_id AND v.source_type = 'opening_balance'
               AND v.status = 'posted' AND v.reversal_of IS NULL
               AND NOT EXISTS (SELECT 1 FROM vouchers r WHERE r.reversal_of = v.id));

-- name: InsertOpeningReceivable :exec
INSERT INTO accounts_receivable (company_id, customer_id, source_type, source_id, source_no, doc_date, due_date,
                                 currency, exchange_rate, amount, base_amount, created_by, import_batch_id)
VALUES (@company_id, @customer_id, 'opening', @source_id, @source_no, @doc_date, @due_date, @currency,
        @exchange_rate, @amount, @base_amount, @created_by, @import_batch_id);

-- name: InsertOpeningPayable :exec
INSERT INTO accounts_payable (company_id, supplier_id, source_type, source_id, source_no, doc_date, due_date,
                              currency, exchange_rate, amount, base_amount, created_by, import_batch_id)
VALUES (@company_id, @supplier_id, 'opening', @source_id, @source_no, @doc_date, @due_date, @currency,
        @exchange_rate, @amount, @base_amount, @created_by, @import_batch_id);

-- name: CurrencyActive :many
SELECT code, decimals FROM currencies WHERE is_active;

-- name: GetImportBatch :one
SELECT * FROM import_batches WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: MarkImportBatchUndone :exec
UPDATE import_batches SET undone_by = @undone_by, undone_at = now() WHERE id = @id;

-- name: ReceivableSourceIDsByBatch :many
SELECT source_id FROM accounts_receivable WHERE import_batch_id = @batch_id ORDER BY id;

-- name: PayableSourceIDsByBatch :many
SELECT source_id FROM accounts_payable WHERE import_batch_id = @batch_id ORDER BY id;

-- name: SetImportBatchSummary :exec
UPDATE import_batches SET summary = @summary WHERE id = @id;

-- name: ActiveCustomerCodes :many
SELECT id, code FROM customers WHERE company_id = @company_id AND is_active;

-- name: ActiveSupplierCodes :many
SELECT id, code FROM suppliers WHERE company_id = @company_id AND is_active;

-- name: OpeningReceivableKeys :many
SELECT customer_id AS partner_id, source_no FROM accounts_receivable WHERE company_id = @company_id AND source_type = 'opening';

-- name: OpeningPayableKeys :many
SELECT supplier_id AS partner_id, source_no FROM accounts_payable WHERE company_id = @company_id AND source_type = 'opening';
