package docstate

import (
	"testing"

	"erp/internal/shared/apperr"
)

func TestHappyPath(t *testing.T) {
	s := Draft
	for _, step := range []struct {
		a    Action
		want Status
	}{{Submit, Pending}, {Approve, Approved}, {Post, Posted}, {Close, Closed}} {
		var err error
		if s, err = Transition(s, step.a); err != nil || s != step.want {
			t.Fatalf("%s → %s, err=%v", step.a, s, err)
		}
	}
}

func TestReverse(t *testing.T) {
	cases := []struct {
		from Status
		a    Action
		want Status
	}{
		{Pending, Reject, Draft},
		{Approved, Unapprove, Draft},
		{Posted, Unpost, Approved},
		{Closed, Reopen, Posted},
		{Draft, Void, Voided},
		{Approved, Void, Voided},
	}
	for _, tc := range cases {
		if got, err := Transition(tc.from, tc.a); err != nil || got != tc.want {
			t.Errorf("%s --%s--> %s, err=%v; want %s", tc.from, tc.a, got, err, tc.want)
		}
	}
}

func TestInvalid(t *testing.T) {
	cases := []struct {
		from Status
		a    Action
	}{
		{Draft, Approve}, // 未送審不可核准
		{Draft, Post},    // 未核准不可過帳
		{Posted, Void},   // 已過帳須先反過帳才能作廢
		{Voided, Submit}, // 作廢不可復原
		{Closed, Unpost}, // 結案須先重開
		{Pending, Post},
	}
	for _, tc := range cases {
		got, err := Transition(tc.from, tc.a)
		if e := apperr.As(err); got != tc.from || e == nil || e.Code != "DOC-001" {
			t.Errorf("%s --%s--> 應被拒絕,got %s err=%v", tc.from, tc.a, got, err)
		}
	}
}

func TestEditableAndValid(t *testing.T) {
	if !Editable(Draft) || Editable(Approved) {
		t.Fatal("只有草稿可編輯")
	}
	if !Valid(Voided) || Valid("unknown") {
		t.Fatal("Valid 錯誤")
	}
	for s := range transitions {
		if Labels[s] == "" {
			t.Errorf("狀態 %s 缺少中文名稱", s)
		}
	}
}
