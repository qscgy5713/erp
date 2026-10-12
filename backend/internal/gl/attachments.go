package gl

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

// 傳票附件:原始憑證的掃描檔。每張傳票最多 10 個、每個 5MB;只收 PDF / PNG / JPEG / Excel(.xlsx),
// 同時檢查副檔名與檔頭,下載一律以附件方式送出(不在瀏覽器內直接執行)。
const (
	maxAttachmentBytes = 5 << 20
	maxAttachments     = 10
)

type attachmentDTO struct {
	ID             int64     `json:"id"`
	Filename       string    `json:"filename"`
	ContentType    string    `json:"content_type"`
	SizeBytes      int32     `json:"size_bytes"`
	UploadedAt     time.Time `json:"uploaded_at"`
	UploadedByName *string   `json:"uploaded_by_name"`
}

// attachmentType 依副檔名與檔頭判斷類型;不接受的回傳空字串。
func attachmentType(name string, head []byte) string {
	detected := http.DetectContentType(head)
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		if bytes.HasPrefix(head, []byte("%PDF-")) {
			return "application/pdf"
		}
	case ".png":
		if detected == "image/png" {
			return "image/png"
		}
	case ".jpg", ".jpeg":
		if detected == "image/jpeg" {
			return "image/jpeg"
		}
	case ".xlsx":
		if bytes.HasPrefix(head, []byte("PK\x03\x04")) {
			return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		}
	}
	return ""
}

func (m *Module) listAttachments(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	if _, err := loadVoucher(ctx, m.store.Queries, companyID, id); err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.ListVoucherAttachments(ctx, db.ListVoucherAttachmentsParams{CompanyID: companyID, VoucherID: id})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]attachmentDTO, len(rows))
	for i, r := range rows {
		out[i] = attachmentDTO{ID: r.ID, Filename: r.Filename, ContentType: r.ContentType, SizeBytes: r.SizeBytes, UploadedAt: r.UploadedAt, UploadedByName: r.UploadedByName}
	}
	response.OK(c, out)
}

func (m *Module) uploadAttachment(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	v, err := loadVoucher(ctx, m.store.Queries, a.CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	if v.Status == "voided" {
		response.Error(c, apperr.Conflict("GL-050", "已作廢的傳票不能再新增附件"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAttachmentBytes+1<<20)
	fh, err := c.FormFile("file")
	if err != nil {
		response.Error(c, fieldErr("file", "請選擇要上傳的檔案(PDF、PNG、JPEG 或 .xlsx,5MB 以內)"))
		return
	}
	if fh.Size <= 0 || fh.Size > maxAttachmentBytes {
		response.Error(c, fieldErr("file", "檔案大小須在 5MB 以內且不可為空"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Error(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(f); err != nil {
		response.Error(c, err)
		return
	}
	name := filepath.Base(strings.ReplaceAll(fh.Filename, `\`, "/"))
	if len([]rune(name)) > 200 {
		response.Error(c, fieldErr("file", "檔名過長(最多 200 字)"))
		return
	}
	ct := attachmentType(name, buf.Bytes()[:min(buf.Len(), 512)])
	if ct == "" {
		response.Error(c, fieldErr("file", "只接受 PDF、PNG、JPEG 與 .xlsx,且內容須與副檔名相符"))
		return
	}
	var out attachmentDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		n, err := q.CountVoucherAttachments(ctx, db.CountVoucherAttachmentsParams{CompanyID: a.CompanyID, VoucherID: id})
		if err != nil {
			return err
		}
		if n >= maxAttachments {
			return apperr.Conflict("GL-051", fmt.Sprintf("每張傳票最多 %d 個附件", maxAttachments))
		}
		r, err := q.CreateVoucherAttachment(ctx, db.CreateVoucherAttachmentParams{
			CompanyID: a.CompanyID, VoucherID: id, Filename: name, ContentType: ct, SizeBytes: int32(buf.Len()),
			Content: buf.Bytes(), UploadedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		out = attachmentDTO{ID: r.ID, Filename: r.Filename, ContentType: r.ContentType, SizeBytes: r.SizeBytes, UploadedAt: r.UploadedAt}
		return audit.Record(ctx, q, audit.Entry{
			Action: "attach", EntityType: "voucher", EntityID: &id, Summary: "傳票 " + v.DocNo + " 新增附件:" + name,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, out)
}

func (m *Module) downloadAttachment(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	aid, err := strconv.ParseInt(c.Param("aid"), 10, 64)
	if err != nil || aid <= 0 {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	row, err := m.store.GetVoucherAttachment(c.Request.Context(), db.GetVoucherAttachmentParams{ID: aid, VoucherID: id, CompanyID: actor(c).CompanyID})
	if err != nil {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(row.Filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, row.ContentType, row.Content)
}

func (m *Module) deleteAttachment(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	aid, err := strconv.ParseInt(c.Param("aid"), 10, 64)
	if err != nil || aid <= 0 {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	v, err := loadVoucher(ctx, m.store.Queries, a.CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	if v.Status != "draft" {
		response.Error(c, apperr.Conflict("GL-052", "已過帳或作廢的傳票,附件是留存的憑證,不能刪除"))
		return
	}
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		n, err := q.DeleteVoucherAttachment(ctx, db.DeleteVoucherAttachmentParams{ID: aid, VoucherID: id, CompanyID: a.CompanyID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.ErrNotFound
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "detach", EntityType: "voucher", EntityID: &id, Summary: "傳票 " + v.DocNo + " 刪除附件 #" + strconv.FormatInt(aid, 10),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"id": aid, "deleted": true})
}
