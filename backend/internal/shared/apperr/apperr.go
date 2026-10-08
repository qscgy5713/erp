// Package apperr 定義帶 HTTP 狀態與錯誤碼的業務錯誤。
// service 層回傳 *Error,handler 透過 response.Error 統一輸出;
// 其他錯誤一律視為內部錯誤(500),細節只寫 log 不回給前端。
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// WithDetails 回傳附帶細節的複本,不修改共用的錯誤變數。
func (e *Error) WithDetails(details any) *Error {
	cp := *e
	cp.Details = details
	return &cp
}

// WithMessage 回傳換了訊息(例如補上具體料品與數量)的複本。
func (e *Error) WithMessage(msg string) *Error {
	cp := *e
	cp.Message = msg
	return &cp
}

func BadRequest(code, message string) *Error   { return New(http.StatusBadRequest, code, message) }
func Unauthorized(code, message string) *Error { return New(http.StatusUnauthorized, code, message) }
func Forbidden(code, message string) *Error    { return New(http.StatusForbidden, code, message) }
func NotFound(code, message string) *Error     { return New(http.StatusNotFound, code, message) }
func Conflict(code, message string) *Error     { return New(http.StatusConflict, code, message) }

// 共用錯誤
var (
	ErrValidation = New(http.StatusUnprocessableEntity, "SYS-422", "輸入資料有誤")
	ErrNotFound   = NotFound("SYS-404", "找不到資源")
	// ErrVersionConflict 樂觀鎖失敗:資料已被他人修改。
	ErrVersionConflict = Conflict("SYS-409", "資料已被其他人修改,請重新整理後再試")
	ErrUnauthorized    = Unauthorized("SYS-401", "請重新登入")
	ErrForbidden       = Forbidden("SYS-403", "沒有權限執行此操作")
	ErrTooManyRequests = New(http.StatusTooManyRequests, "SYS-429", "請求過於頻繁,請稍後再試")
)

// Validation 回傳欄位驗證錯誤,fields 為「欄位 → 訊息」。
func Validation(fields map[string]string) *Error {
	return ErrValidation.WithDetails(fields)
}

// As 取出 *Error;不是業務錯誤時回傳 nil。
func As(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}
