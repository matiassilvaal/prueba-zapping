package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"prueba-zapping/internal/auth"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestServer(t *testing.T) (*Server, *auth.Service) {
	t.Helper()
	a := auth.NewService(auth.NewMemoryUserStore(), auth.NewMemorySessionStore(), time.Hour)
	s, err := New(Deps{
		Auth:       a,
		Stream:     http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "STREAM "+r.URL.Path) }),
		Ready:      func(context.Context) error { return nil },
		SessionTTL: time.Hour,
		Logger:     quietLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, a
}

func postForm(h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("sin cookie de sesión")
	return nil
}

func TestRegister_GET(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/register", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Crear cuenta") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestRegister_POST(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	ok := url.Values{"name": {"Ana"}, "email": {"ana@example.com"}, "password": {"secreto123"}}

	rec := postForm(h, "/register", ok)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/player" {
		t.Fatalf("registro ok: status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	sessionCookie(t, rec)

	rec = postForm(h, "/register", ok)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Ya existe una cuenta") {
		t.Fatalf("duplicado: status %d", rec.Code)
	}

	rec = postForm(h, "/register", url.Values{"name": {""}, "email": {"no-es-email"}, "password": {"123"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("inválido: status %d", rec.Code)
	}
	for _, msg := range []string{"El nombre es obligatorio", "El email no es válido", "al menos 8 caracteres", `value="no-es-email"`} {
		if !strings.Contains(rec.Body.String(), msg) {
			t.Errorf("falta %q en el formulario", msg)
		}
	}
}

func TestStatic(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/static/app.css", nil))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("status %d ct %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("cache-control %q", rec.Header().Get("Cache-Control"))
	}
}
