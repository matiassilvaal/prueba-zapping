package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func runningService(t *testing.T) *Service {
	t.Helper()
	tl := testTimeline(t)
	svc := NewService(tl, memLoader(nil), newFakeClock(time.Unix(0, 0)), quietLogger())
	events, cancel := svc.Subscribe()
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(func() { stop(); cancel() })
	go svc.Run(ctx)
	waitWindow(t, events)
	return svc
}

func TestHandler_PlaylistNoLista(t *testing.T) {
	svc := NewService(testTimeline(t), memLoader(nil), newFakeClock(time.Unix(0, 0)), quietLogger())
	rec := httptest.NewRecorder()
	NewHandler(svc).ServeHTTP(rec, httptest.NewRequest("GET", "/playlist.m3u8", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHandler_Playlist(t *testing.T) {
	h := NewHandler(runningService(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/playlist.m3u8", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("content-type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-cache" {
		t.Errorf("cache-control %q", cc)
	}
	if rec.Header().Get("ETag") != `"0"` {
		t.Errorf("etag %q", rec.Header().Get("ETag"))
	}
	req := httptest.NewRequest("GET", "/playlist.m3u8", nil)
	req.Header.Set("If-None-Match", `"0"`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("304 esperado, got %d", rec.Code)
	}
}

func TestHandler_Segmento(t *testing.T) {
	h := NewHandler(runningService(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/a.ts", nil))
	if rec.Code != 200 || rec.Body.String() != "bytes de a.ts" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("content-type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=3600, immutable" {
		t.Errorf("cache-control %q", cc)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/zzz.ts", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("404 esperado, got %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/a.ts", nil)
	req.Header.Set("Range", "bytes=0-4")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "bytes" {
		t.Fatalf("range: status %d body %q", rec.Code, rec.Body.String())
	}
}
