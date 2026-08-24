package web

import (
	"context"
	"errors"
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

func registerAndLogin(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := postForm(h, "/register", url.Values{"name": {"Ana"}, "email": {"ana@example.com"}, "password": {"secreto123"}})
	return sessionCookie(t, rec)
}

func TestLogin(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	registerAndLogin(t, h)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Iniciar sesión") {
		t.Fatalf("GET: %d", rec.Code)
	}

	rec = postForm(h, "/login", url.Values{"email": {"ana@example.com"}, "password": {"incorrecta"}})
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Email o contraseña incorrectos") {
		t.Fatalf("credenciales inválidas: %d", rec.Code)
	}

	rec = postForm(h, "/login", url.Values{"email": {"ana@example.com"}, "password": {"secreto123"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/player" {
		t.Fatalf("login ok: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	c := sessionCookie(t, rec)

	req := httptest.NewRequest("GET", "/login", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/player" {
		t.Fatalf("login con sesión debía redirigir: %d", rec.Code)
	}
}

func TestLogoutYRaiz(t *testing.T) {
	s, a := newTestServer(t)
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("raíz sin sesión: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	c := registerAndLogin(t, h)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Location") != "/player" {
		t.Fatalf("raíz con sesión: %q", rec.Header().Get("Location"))
	}

	req = httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("logout: %d", rec.Code)
	}
	if cleared := rec.Result().Cookies()[0]; cleared.MaxAge != -1 {
		t.Fatalf("la cookie debía borrarse: %+v", cleared)
	}
	if _, err := a.Authenticate(context.Background(), c.Value); err == nil {
		t.Fatal("la sesión debía quedar invalidada")
	}
}

func TestPlayerYStreamProtegidos(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/player", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("player sin sesión: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/stream/playlist.m3u8", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stream sin sesión: %d", rec.Code)
	}

	c := registerAndLogin(t, h)
	req := httptest.NewRequest("GET", "/player", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `src="/static/vendor/hls.min.js"`) || !strings.Contains(rec.Body.String(), `id="video"`) {
		t.Fatalf("player con sesión: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/static/vendor/hls.min.js", nil))
	if rec.Code != 200 || rec.Body.Len() < 100_000 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("hls.min.js embebido: status %d bytes %d ct %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}
	req = httptest.NewRequest("GET", "/stream/playlist.m3u8", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "STREAM /playlist.m3u8" {
		t.Fatalf("stream con sesión: %d %q", rec.Code, rec.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	s.deps.Ready = func(context.Context) error { return errors.New("stream no listo") }
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}
