// Package response 定義 API 統一回應格式:{ "data": ..., "meta": ..., "error": ... }。
package response

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"erp/internal/shared/apperr"
)

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type Body struct {
	Data  any        `json:"data,omitempty"`
	Meta  any        `json:"meta,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Data: data})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Body{Data: data})
}

func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func List(c *gin.Context, data, meta any) {
	c.JSON(http.StatusOK, Body{Data: data, Meta: meta})
}

func Fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, Body{Error: &ErrorBody{Code: code, Message: message}})
}

// Error 輸出錯誤:業務錯誤照原樣回傳,其他錯誤記 log 後回 500,不外洩內部訊息。
func Error(c *gin.Context, err error) {
	if e := apperr.As(err); e != nil {
		c.AbortWithStatusJSON(e.Status, Body{Error: &ErrorBody{Code: e.Code, Message: e.Message, Details: e.Details}})
		return
	}
	slog.ErrorContext(c.Request.Context(), "內部錯誤",
		"err", err,
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"request_id", c.GetString(RequestIDKey),
	)
	Fail(c, http.StatusInternalServerError, "SYS-500", "系統發生錯誤,請稍後再試")
}

// RequestIDKey 為 gin.Context 中存放 request id 的 key。
const RequestIDKey = "request_id"
