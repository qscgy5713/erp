// Package migrations 內嵌所有 migration SQL,讓 CLI 能直接為新租戶資料庫建表與升級
// (格式與 golang-migrate 相同,所以也能繼續用 migrate 指令)。
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
