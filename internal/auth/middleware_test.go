package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeAuth struct{ valid map[string]int64 }

func (f fakeAuth) Authenticate(_ context.Context, token string) (int64, error) {
	if id, ok := f.valid[token]; ok {
		return id, nil
	}
	return 0, ErrNotFound
}

func TestRequireSession(t *testing.T) {
	a := fakeAuth{valid: map[string]int64{"ok": 42}}
	var seen int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = UserID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	t.Run("con sesión", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/player", nil)
		req.AddCookie(&http.Cookie{Name: CookieName, Value: "ok"})
		rec := httptest.NewRecorder()
		RequireSession(a, RedirectToLogin, next).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || seen != 42 {
			t.Fatalf("status %d user %d", rec.Code, seen)
		}
	})
	t.Run("sin sesión redirige", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RequireSession(a, RedirectToLogin, next).ServeHTTP(rec, httptest.NewRequest("GET", "/player", nil))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("status %d location %q", rec.Code, rec.Header().Get("Location"))
		}
	})
	t.Run("sin sesión 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stream/playlist.m3u8", nil)
		req.AddCookie(&http.Cookie{Name: CookieName, Value: "malo"})
		rec := httptest.NewRecorder()
		RequireSession(a, Unauthorized, next).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("status %d ct %q", rec.Code, rec.Header().Get("Content-Type"))
		}
	})
}

func TestSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSessionCookie(rec, "tok", time.Hour, true)
	c := rec.Result().Cookies()[0]
	if c.Name != CookieName || c.Value != "tok" || !c.HttpOnly || !c.Secure || c.Path != "/" ||
		c.SameSite != http.SameSiteLaxMode || c.MaxAge != 3600 {
		t.Fatalf("cookie inesperada: %+v", c)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)
	if TokenFromRequest(req) != "tok" {
		t.Fatal("TokenFromRequest")
	}
	rec = httptest.NewRecorder()
	ClearSessionCookie(rec, false)
	if c := rec.Result().Cookies()[0]; c.MaxAge != -1 || c.Value != "" {
		t.Fatalf("clear: %+v", c)
	}
}
