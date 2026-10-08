package trade

import (
	"testing"

	"erp/internal/shared/docstate"
)

func TestOrderTransition(t *testing.T) {
	ok := []struct {
		from docstate.Status
		a    docstate.Action
		want docstate.Status
	}{
		{docstate.Approved, docstate.Close, docstate.Closed},
		{docstate.Closed, docstate.Reopen, docstate.Approved}, // 重開回已核准,不是已過帳
		{docstate.Approved, docstate.Unapprove, docstate.Draft},
	}
	for _, tc := range ok {
		if got, err := OrderTransition(tc.from, tc.a); err != nil || got != tc.want {
			t.Errorf("%s --%s--> %s, err=%v; want %s", tc.from, tc.a, got, err, tc.want)
		}
	}
	for _, a := range []docstate.Action{docstate.Post, docstate.Unpost} {
		if _, err := OrderTransition(docstate.Approved, a); err == nil {
			t.Errorf("訂單類單據不可 %s", a)
		}
	}
	if _, err := OrderTransition(docstate.Draft, docstate.Reopen); err == nil {
		t.Error("草稿不可重開")
	}
}

func TestPostingTransition(t *testing.T) {
	if got, err := PostingTransition(docstate.Approved, docstate.Post); err != nil || got != docstate.Posted {
		t.Fatalf("post: %s %v", got, err)
	}
	for _, a := range []docstate.Action{docstate.Close, docstate.Reopen} {
		if _, err := PostingTransition(docstate.Approved, a); err == nil {
			t.Errorf("過帳類單據不可 %s", a)
		}
	}
}
