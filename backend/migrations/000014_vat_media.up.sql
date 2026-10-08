-- 營業稅媒體申報檔所需資料(D62)
-- 公司稅籍編號(9 碼,申報檔每筆記錄都要填)
ALTER TABLE companies ADD COLUMN tax_reg_no VARCHAR(9) NOT NULL DEFAULT '';
ALTER TABLE companies ADD CONSTRAINT companies_tax_reg_no_format CHECK (tax_reg_no = '' OR tax_reg_no ~ '^[A-Z0-9]{9}$');

-- 進貨單的供應商發票:發票日期與憑證種類(字軌號碼沿用 invoice_no,格式在 API 層驗證)
--   triplicate  三聯式 / 電子計算機統一發票 → 格式代號 21
--   register2   二聯式收銀機統一發票       → 22
--   register3   三聯式收銀機統一發票或電子發票 → 25
ALTER TABLE goods_receipts ADD COLUMN invoice_date DATE;
ALTER TABLE goods_receipts ADD COLUMN invoice_kind VARCHAR(10) NOT NULL DEFAULT '';
ALTER TABLE goods_receipts ADD CONSTRAINT goods_receipts_invoice_kind_check
    CHECK (invoice_kind IN ('', 'triplicate', 'register2', 'register3'));
