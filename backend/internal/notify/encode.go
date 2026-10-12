package notify

import (
	"encoding/base64"
	"strings"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// wrap76 依 MIME 規定每 76 字元斷行。
func wrap76(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}
