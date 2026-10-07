// Package response 定義 API 統一回應格式:{ "data": ..., "meta": ..., "error": ... }。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type Body struct {
	Data  any    `json:"data,omitempty"`
	Meta  any    `json:"meta,omitempty"`
	Error *Error `json:"error,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Data: data})
}

func List(c *gin.Context, data, meta any) {
	c.JSON(http.StatusOK, Body{Data: data, Meta: meta})
}

func Fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, Body{Error: &Error{Code: code, Message: message}})
}
