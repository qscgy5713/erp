package apperr

import (
	"fmt"
	"testing"
)

func TestAsUnwrapsWrappedError(t *testing.T) {
	err := fmt.Errorf("建立使用者: %w", ErrVersionConflict)
	got := As(err)
	if got == nil || got.Code != "SYS-409" {
		t.Fatalf("As = %v", got)
	}
	if As(fmt.Errorf("plain")) != nil {
		t.Fatal("一般錯誤不應被視為業務錯誤")
	}
}

func TestWithDetailsDoesNotMutateShared(t *testing.T) {
	_ = Validation(map[string]string{"name": "必填"})
	if ErrValidation.Details != nil {
		t.Fatal("WithDetails 不應修改共用錯誤變數")
	}
}
