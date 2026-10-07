// Package page 處理列表 API 的分頁參數。
package page

import "strconv"

const (
	DefaultSize = 20
	MaxSize     = 100
)

type Params struct {
	Page int
	Size int
}

// Meta 為列表回應的 meta。
type Meta struct {
	Page  int   `json:"page"`
	Size  int   `json:"size"`
	Total int64 `json:"total"`
}

// Parse 解析 page / size 字串;不合法時採預設值,size 上限 MaxSize。
func Parse(pageStr, sizeStr string) Params {
	p, err := strconv.Atoi(pageStr)
	if err != nil || p < 1 {
		p = 1
	}
	s, err := strconv.Atoi(sizeStr)
	if err != nil || s < 1 {
		s = DefaultSize
	}
	if s > MaxSize {
		s = MaxSize
	}
	return Params{Page: p, Size: s}
}

func (p Params) Limit() int32  { return int32(p.Size) }
func (p Params) Offset() int32 { return int32((p.Page - 1) * p.Size) }

func (p Params) Meta(total int64) Meta {
	return Meta{Page: p.Page, Size: p.Size, Total: total}
}
