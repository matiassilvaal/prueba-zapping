package web

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"prueba-zapping/internal/auth"
	"prueba-zapping/internal/stream"
)

// Levanta el stack completo como en main (stream + hub + auth en memoria +
// Recover → Logging → CSRF), con el hub suscrito antes de arrancar el worker
// para recibir la primera ventana por contrato
//
// @param [*testing.T] t: test
// @param [context.Context] ctx: cancelación de los workers
//
// @return [*httptest.Server] servidor (se cierra solo al terminar el test)
// @return [*http.Client] cliente con cookies que no sigue redirects
// @return [*Hub] hub SSE
func newE2EStack(t *testing.T, ctx context.Context) (*httptest.Server, *http.Client, *Hub) {
	t.Helper()
	tl, err := stream.NewTimeline([]stream.Segment{
		{Name: "s0.ts", Duration: 10 * time.Second}, {Name: "s1.ts", Duration: 10 * time.Second},
		{Name: "s2.ts", Duration: 10 * time.Second}, {Name: "s3.ts", Duration: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	loader := func(name string) ([]byte, error) { return []byte("video " + name), nil }
	streamSvc := stream.NewService(tl, loader, stream.RealClock(), quietLogger())
	events, unsub := streamSvc.Subscribe()
	t.Cleanup(unsub)
	hub := NewHub(quietLogger())
	go hub.Run(ctx, events)
	go streamSvc.Run(ctx)

	authSvc := auth.NewService(auth.NewMemoryUserStore(), auth.NewMemorySessionStore(), time.Hour)
	site, err := New(Deps{
		Auth: authSvc, Stream: stream.NewHandler(streamSvc), Hub: hub,
		Ready: func(context.Context) error {
			if streamSvc.Snapshot() == nil {
				return io.ErrUnexpectedEOF
			}
			return nil
		},
		Logger: quietLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Recover(quietLogger())(Logging(quietLogger())(http.NewCrossOriginProtection().Handler(site.Handler()))))
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return srv, client, hub
}

// Registra un usuario por el formulario (mismo origen: pasa la protección CSRF)
// y deja la cookie de sesión en el jar del cliente
//
// @param [*testing.T] t: test
// @param [*http.Client] client: cliente con jar
// @param [string] baseURL: URL del servidor de test
func registerUser(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	form := url.Values{"name": {"Ana"}, "email": {"ana@example.com"}, "password": {"secreto123"}}
	resp, err := client.PostForm(baseURL+"/register", form)
	if err != nil {
		t.Fatalf("registro: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("registro: status %d", resp.StatusCode)
	}
}

func TestE2E_FlujoCompleto(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, client, _ := newE2EStack(t, ctx)

	get := func(path string) *http.Response {
		t.Helper()
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	body := func(resp *http.Response) string {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	// Esperar a que el stream publique su primer snapshot.
	for i := 0; i < 100; i++ {
		if resp := get("/healthz"); resp.StatusCode == 200 {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Sin sesión: player redirige, stream 401.
	if resp := get("/player"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("player sin sesión: %d", resp.StatusCode)
	}
	if resp := get("/stream/playlist.m3u8"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stream sin sesión: %d", resp.StatusCode)
	}

	registerUser(t, client, srv.URL)

	if resp := get("/player"); resp.StatusCode != 200 {
		t.Fatalf("player con sesión: %d", resp.StatusCode)
	}
	if b := body(get("/stream/playlist.m3u8")); !strings.Contains(b, "#EXT-X-MEDIA-SEQUENCE:0") || !strings.Contains(b, "s0.ts") {
		t.Fatalf("playlist: %q", b)
	}
	if b := body(get("/stream/s0.ts")); b != "video s0.ts" {
		t.Fatalf("segmento: %q", b)
	}
	if resp := get("/stream/no-existe.ts"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("fuera de ventana: %d", resp.StatusCode)
	}

	// Logout y verificación.
	resp, err := client.PostForm(srv.URL+"/logout", nil)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()
	if resp := get("/player"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("player tras logout: %d", resp.StatusCode)
	}
	if resp := get("/stream/playlist.m3u8"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stream tras logout: %d", resp.StatusCode)
	}
}

// El SSE autenticado debe funcionar a través del stack completo de middlewares
// (Recover → Logging → CSRF): fija por contrato que statusWriter preserva
// Flush en el flujo real, no solo en el type-assert que prueba
// TestLogging_PreservaFlusher.
func TestE2E_EventsPorElStackCompleto(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, client, hub := newE2EStack(t, ctx)

	registerUser(t, client, srv.URL)

	// Esperar a que el hub tenga la primera ventana (evento inicial de /events).
	for i := 0; hub.lastWindow() == nil && i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if hub.lastWindow() == nil {
		t.Fatal("el hub no recibió la primera ventana")
	}

	resp, err := client.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("events: content-type %q", ct)
	}
	data := readEvent(t, bufio.NewScanner(resp.Body), "window")
	if !strings.Contains(data, `"sequence":`) || !strings.Contains(data, `"viewers":`) {
		t.Fatalf("evento window inesperado: %s", data)
	}
}
