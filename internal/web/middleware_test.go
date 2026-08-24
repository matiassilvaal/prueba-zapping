package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecover(t *testing.T) {
	h := Recover(quietLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLogging_PreservaFlusher(t *testing.T) {
	var isFlusher bool
	h := Logging(quietLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, isFlusher = w.(http.Flusher)
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !isFlusher || rec.Code != http.StatusTeapot {
		t.Fatalf("flusher=%v status=%d", isFlusher, rec.Code)
	}
}
