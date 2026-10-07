// Package docstate 定義所有單據共用的狀態機。
//
//	草稿 ──送審──▶ 待審 ──核准──▶ 已核准 ──過帳──▶ 已過帳 ──結案──▶ 已結案
//	  ▲             │退回            │反核准             │反過帳
//	  └─────────────┘                ▼                   ▼
//	                       (回到草稿 / 已核准);草稿、待審、已核准可作廢
//
// 「有無下游單據」「是否已關帳」等條件由各模組在呼叫 Transition 前自行檢查。
package docstate

import (
	"net/http"

	"erp/internal/shared/apperr"
)

type Status string

const (
	Draft    Status = "draft"
	Pending  Status = "pending"
	Approved Status = "approved"
	Posted   Status = "posted"
	Closed   Status = "closed"
	Voided   Status = "voided"
)

type Action string

const (
	Submit    Action = "submit"
	Reject    Action = "reject"
	Approve   Action = "approve"
	Unapprove Action = "unapprove"
	Post      Action = "post"
	Unpost    Action = "unpost"
	Close     Action = "close"
	Reopen    Action = "reopen"
	Void      Action = "void"
)

var Labels = map[Status]string{
	Draft: "草稿", Pending: "待審", Approved: "已核准", Posted: "已過帳", Closed: "已結案", Voided: "已作廢",
}

var transitions = map[Status]map[Action]Status{
	Draft:    {Submit: Pending, Void: Voided},
	Pending:  {Approve: Approved, Reject: Draft, Void: Voided},
	Approved: {Post: Posted, Unapprove: Draft, Void: Voided, Close: Closed},
	Posted:   {Unpost: Approved, Close: Closed},
	Closed:   {Reopen: Posted},
	Voided:   {},
}

// ErrInvalidTransition 目前狀態不允許此動作。
var ErrInvalidTransition = apperr.New(http.StatusConflict, "DOC-001", "目前單據狀態不允許此操作")

// Transition 回傳執行動作後的新狀態。
func Transition(from Status, action Action) (Status, error) {
	to, ok := transitions[from][action]
	if !ok {
		return from, ErrInvalidTransition.WithDetails(map[string]string{
			"status": string(from), "action": string(action),
		})
	}
	return to, nil
}

// Editable 只有草稿可以修改內容。
func Editable(s Status) bool { return s == Draft }

// Valid 判斷是否為已知狀態。
func Valid(s Status) bool {
	_, ok := transitions[s]
	return ok
}
