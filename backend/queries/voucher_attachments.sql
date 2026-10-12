-- name: ListVoucherAttachments :many
-- 不含內容(內容只在下載時讀取)
SELECT a.id, a.voucher_id, a.filename, a.content_type, a.size_bytes, a.uploaded_at, u.name AS uploaded_by_name
FROM voucher_attachments a
LEFT JOIN users u ON u.id = a.uploaded_by
WHERE a.company_id = @company_id AND a.voucher_id = @voucher_id
ORDER BY a.id;

-- name: GetVoucherAttachment :one
SELECT * FROM voucher_attachments WHERE id = @id AND voucher_id = @voucher_id AND company_id = @company_id;

-- name: CountVoucherAttachments :one
SELECT count(*) FROM voucher_attachments WHERE company_id = @company_id AND voucher_id = @voucher_id;

-- name: CreateVoucherAttachment :one
INSERT INTO voucher_attachments (company_id, voucher_id, filename, content_type, size_bytes, content, uploaded_by)
VALUES (@company_id, @voucher_id, @filename, @content_type, @size_bytes, @content, @uploaded_by)
RETURNING id, voucher_id, filename, content_type, size_bytes, uploaded_at;

-- name: DeleteVoucherAttachment :execrows
DELETE FROM voucher_attachments WHERE id = @id AND voucher_id = @voucher_id AND company_id = @company_id;
