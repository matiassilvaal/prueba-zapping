package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"prueba-zapping/internal/stream"
)

// readEvent lee líneas hasta encontrar "event: <name>" y devuelve su línea data.
func readEvent(t *testing.T, sc *bufio.Scanner, name string) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("no llegó el evento %q", name)
		default:
		}
		if !sc.Scan() {
			t.Fatalf("stream cerrado esperando %q: %v", name, sc.Err())
		}
		if sc.Text() == "event: "+name {
			sc.Scan()
			return strings.TrimPrefix(sc.Text(), "data: ")
		}
	}
}

func TestHub(t *testing.T) {
	hub := NewHub(quietLogger())
	events := make(chan stream.Window, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx, events)

	srv := httptest.NewServer(hub)
	defer srv.Close()

	reqCtx, cancelReq := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(reqCtx, "GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)

	if data := readEvent(t, sc, "viewers"); data != `{"viewers":1}` {
		t.Fatalf("viewers: %s", data)
	}
	events <- stream.Window{
		MediaSequence: 5, DiscontinuitySequence: 1,
		Entries:  []stream.Entry{{Name: "a.ts", Duration: 10 * time.Second}, {Name: "b.ts", Duration: 10 * time.Second, Discontinuity: true}, {Name: "c.ts", Duration: 4 * time.Second}},
		NextTick: time.Now().Add(7 * time.Second),
	}
	data := readEvent(t, sc, "window")
	for _, want := range []string{`"sequence":5`, `"discontinuitySequence":1`, `"name":"b.ts"`, `"discontinuity":true`, `"viewers":1`, `"secondsToNextTick":`} {
		if !strings.Contains(data, want) {
			t.Errorf("falta %s en %s", want, data)
		}
	}
	if hub.Viewers() != 1 {
		t.Fatalf("viewers: %d", hub.Viewers())
	}
	cancelReq()
	for i := 0; i < 50 && hub.Viewers() != 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if hub.Viewers() != 0 {
		t.Fatal("el cliente debía darse de baja al desconectar")
	}
}

func TestHub_ClienteNuevoRecibeUltimaVentana(t *testing.T) {
	hub := NewHub(quietLogger())
	events := make(chan stream.Window, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx, events)
	events <- stream.Window{MediaSequence: 9, Entries: []stream.Entry{{Name: "x.ts"}, {Name: "y.ts"}, {Name: "z.ts"}}, NextTick: time.Now()}
	for i := 0; i < 50 && hub.lastWindow() == nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}

	srv := httptest.NewServer(hub)
	defer srv.Close()
	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	req, _ := http.NewRequestWithContext(reqCtx, "GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if data := readEvent(t, bufio.NewScanner(resp.Body), "window"); !strings.Contains(data, `"sequence":9`) {
		t.Fatalf("ventana inicial: %s", data)
	}
}
