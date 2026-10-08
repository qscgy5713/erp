ALTER TABLE goods_receipts DROP CONSTRAINT goods_receipts_invoice_kind_check;
ALTER TABLE goods_receipts DROP COLUMN invoice_kind;
ALTER TABLE goods_receipts DROP COLUMN invoice_date;
ALTER TABLE companies DROP CONSTRAINT companies_tax_reg_no_format;
ALTER TABLE companies DROP COLUMN tax_reg_no;
