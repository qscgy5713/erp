-- 委外加工費的進項稅額與供應商發票(D103):有稅別的收回會併入營業稅 401 申報
-- processing_fee 改為「未稅」加工費(沒選稅別時稅額為 0,與舊資料一致);應付金額 = 加工費 + 稅額
ALTER TABLE subcontract_txns
    ADD COLUMN tax_type_id  BIGINT        REFERENCES tax_types (id),
    ADD COLUMN tax_amount   NUMERIC(18,2) NOT NULL DEFAULT 0,
    ADD COLUMN invoice_no   VARCHAR(20)   NOT NULL DEFAULT '',
    ADD COLUMN invoice_date DATE,
    ADD COLUMN invoice_kind VARCHAR(10)   NOT NULL DEFAULT '';
ALTER TABLE subcontract_txns ADD CONSTRAINT subcontract_txns_invoice_kind_check
    CHECK (invoice_kind IN ('', 'triplicate', 'register2', 'register3'));
ALTER TABLE subcontract_txns ADD CONSTRAINT subcontract_txns_tax_check CHECK (tax_amount >= 0);
