package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func newRouter(t *testing.T, d Deps) *gin.Engine {
	t.Helper()
	d.Production = true
	r, err := NewRouter(d)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return r
}

func TestHealth(t *testing.T) {
	cases := []struct {
		name       string
		db         fakeDB
		wantStatus int
		wantBody   string
	}{
		{"資料庫正常", fakeDB{}, http.StatusOK, `{"data":{"status":"ok"}}`},
		{"資料庫異常", fakeDB{err: errors.New("down")}, http.StatusServiceUnavailable, `"code":"SYS-503"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t, Deps{DB: tc.db})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Fatalf("body = %s, want contains %s", w.Body.String(), tc.wantBody)
			}
			if strings.Contains(w.Body.String(), "down") {
				t.Fatalf("內部錯誤訊息不應外洩: %s", w.Body.String())
			}
		})
	}
}

func TestNoRoute(t *testing.T) {
	r := newRouter(t, Deps{DB: fakeDB{}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "SYS-404") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name    string
		trusted []string
		remote  string
		want    string
	}{
		{"未設定可信 proxy 時忽略 X-Forwarded-For", nil, "10.0.0.5:1234", "10.0.0.5"},
		{"來自可信 proxy 時採用 X-Forwarded-For", []string{"10.0.0.0/8"}, "10.0.0.5:1234", "1.2.3.4"},
		{"來自非可信位址時忽略 X-Forwarded-For", []string{"10.0.0.0/8"}, "192.168.1.9:1234", "192.168.1.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t, Deps{DB: fakeDB{}, TrustedProxies: tc.trusted})
			r.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

			req := httptest.NewRequest(http.MethodGet, "/ip", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-For", "1.2.3.4")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if got := w.Body.String(); got != tc.want {
				t.Fatalf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewRouterRejectsInvalidProxy(t *testing.T) {
	if _, err := NewRouter(Deps{DB: fakeDB{}, Production: true, TrustedProxies: []string{"not-an-ip"}}); err == nil {
		t.Fatal("無效的 TRUSTED_PROXIES 應回傳錯誤")
	}
}
