package web

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecover_RelanzaErrAbortHandler(t *testing.T) {
	h := Recover(quietLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("debía re-lanzar http.ErrAbortHandler, recuperó %v", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	t.Fatal("no hubo panic")
}

func TestRecover_NoEscribeSobreRespuestaIniciada(t *testing.T) {
	h := Recover(quietLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: hola\n\n")
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if body := rec.Body.String(); strings.Contains(body, "Error interno") {
		t.Fatalf("no debía escribir el error sobre una respuesta ya iniciada (SSE): %q", body)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLogging_StatusPorDefecto200(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := Logging(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if out := buf.String(); !strings.Contains(out, "status=200") {
		t.Fatalf("un handler que no escribe responde 200 implícito; el log dice: %s", out)
	}
}
