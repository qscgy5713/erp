// Package httpx 提供 handler 共用的請求處理:參數綁定與驗證、路徑參數、request id。
package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
)

func init() {
	// 驗證錯誤的欄位名稱改用 json tag,與前端欄位一致
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
	}
}

// BindJSON 解析並驗證 JSON body;失敗時回傳 SYS-422 與各欄位訊息。
func BindJSON(c *gin.Context, dst any) error {
	if err := c.ShouldBindJSON(dst); err != nil {
		var verrs validator.ValidationErrors
		if errors.As(err, &verrs) {
			fields := make(map[string]string, len(verrs))
			for _, fe := range verrs {
				fields[fe.Field()] = message(fe)
			}
			return apperr.Validation(fields)
		}
		return apperr.BadRequest("SYS-400", "請求格式錯誤")
	}
	return nil
}

func message(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "必填"
	case "min":
		if fe.Kind() == reflect.String {
			return "至少 " + fe.Param() + " 個字"
		}
		return "不可小於 " + fe.Param()
	case "max":
		if fe.Kind() == reflect.String {
			return "最多 " + fe.Param() + " 個字"
		}
		return "不可大於 " + fe.Param()
	case "email":
		return "Email 格式錯誤"
	case "oneof":
		return "必須是下列其中之一:" + fe.Param()
	case "numeric":
		return "只能是數字"
	case "len":
		return "長度必須為 " + fe.Param()
	case "alphanum":
		return "只能是英文或數字"
	default:
		return "格式錯誤"
	}
}

// ParamID 取得路徑中的正整數 id。
func ParamID(c *gin.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.ErrNotFound
	}
	return id, nil
}

// QueryInt64 解析選填的整數查詢參數;空字串回傳 nil。
func QueryInt64(c *gin.Context, name string) (*int64, error) {
	s := c.Query(name)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, apperr.Validation(map[string]string{name: "只能是數字"})
	}
	return &v, nil
}

// QueryBool 解析選填的布林查詢參數(true/false);空字串回傳 nil。
func QueryBool(c *gin.Context, name string) (*bool, error) {
	s := c.Query(name)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return nil, apperr.Validation(map[string]string{name: "只能是 true 或 false"})
	}
	return &v, nil
}

// QueryString 回傳去除空白的查詢參數;空字串回傳 nil。
func QueryString(c *gin.Context, name string) *string {
	s := strings.TrimSpace(c.Query(name))
	if s == "" {
		return nil
	}
	return &s
}

const requestIDHeader = "X-Request-ID"

// RequestMeta 中介層:產生(或沿用合法的)request id,並把請求資訊放進 context。
func RequestMeta() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		c.Set(response.RequestIDKey, id)
		c.Header(requestIDHeader, id)
		c.Request = c.Request.WithContext(authctx.WithMeta(c.Request.Context(), authctx.RequestMeta{
			RequestID: id,
			IP:        c.ClientIP(),
			UserAgent: truncate(c.Request.UserAgent(), 255),
		}))
		c.Next()
	}
}

func validRequestID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !isAlnum(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func isAlnum(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func newRequestID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// 依位元組截斷可能切到多位元組字元,PostgreSQL 會拒絕不合法的 UTF-8
	return strings.ToValidUTF8(s[:n], "")
}
