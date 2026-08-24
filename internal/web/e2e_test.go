package web

import (
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

func TestE2E_FlujoCompleto(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tl, err := stream.NewTimeline([]stream.Segment{
		{Name: "s0.ts", Duration: 10 * time.Second}, {Name: "s1.ts", Duration: 10 * time.Second},
		{Name: "s2.ts", Duration: 10 * time.Second}, {Name: "s3.ts", Duration: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	loader := func(name string) ([]byte, error) { return []byte("video " + name), nil }
	streamSvc := stream.NewService(tl, loader, stream.RealClock(), quietLogger())
	go streamSvc.Run(ctx)
	events, unsub := streamSvc.Subscribe()
	defer unsub()
	hub := NewHub(quietLogger())
	go hub.Run(ctx, events)

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
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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

	// Registro (mismo origen: pasa la protección CSRF).
	form := url.Values{"name": {"Ana"}, "email": {"ana@example.com"}, "password": {"secreto123"}}
	resp, err := client.PostForm(srv.URL+"/register", form)
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("registro: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

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
	resp, _ = client.PostForm(srv.URL+"/logout", nil)
	resp.Body.Close()
	if resp := get("/player"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("player tras logout: %d", resp.StatusCode)
	}
	if resp := get("/stream/playlist.m3u8"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stream tras logout: %d", resp.StatusCode)
	}
}
