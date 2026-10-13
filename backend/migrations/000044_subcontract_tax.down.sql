ALTER TABLE subcontract_txns
    DROP CONSTRAINT subcontract_txns_tax_check,
    DROP CONSTRAINT subcontract_txns_invoice_kind_check,
    DROP COLUMN tax_type_id, DROP COLUMN tax_amount, DROP COLUMN invoice_no, DROP COLUMN invoice_date, DROP COLUMN invoice_kind;
