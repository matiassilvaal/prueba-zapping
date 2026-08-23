package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func memLoader(fail map[string]error) SegmentLoader {
	return func(name string) ([]byte, error) {
		if err, ok := fail[name]; ok {
			return nil, err
		}
		return []byte("bytes de " + name), nil
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func waitWindow(t *testing.T, ch <-chan Window) Window {
	t.Helper()
	select {
	case w := <-ch:
		return w
	case <-time.After(2 * time.Second):
		t.Fatal("no llegó el tick")
		return Window{}
	}
}

func TestService_PublicaYAvanza(t *testing.T) {
	tl := testTimeline(t) // a b c d (10,10,10,4)
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc := NewService(tl, memLoader(nil), clock, quietLogger())
	if svc.Snapshot() != nil {
		t.Fatal("no debe haber snapshot antes de Run")
	}
	events, cancel := svc.Subscribe()
	defer cancel()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	w := waitWindow(t, events)
	if w.MediaSequence != 0 {
		t.Fatalf("primer tick: seq %d", w.MediaSequence)
	}
	snap := svc.Snapshot()
	if snap == nil || !strings.Contains(string(snap.Playlist), "#EXT-X-MEDIA-SEQUENCE:0\n") || snap.ETag != `"0"` {
		t.Fatalf("snapshot inesperado: %+v", snap)
	}
	for _, name := range []string{"a.ts", "b.ts", "c.ts", "d.ts"} {
		if _, ok := svc.Segment(name); !ok {
			t.Errorf("%s debía estar en caché en k=0", name)
		}
	}
	if _, ok := svc.Segment("zzz.ts"); ok {
		t.Error("segmento desconocido no debe existir")
	}

	clock.Advance(10 * time.Second)
	w = waitWindow(t, events)
	if w.MediaSequence != 1 || svc.Snapshot().ETag != `"1"` {
		t.Fatalf("segundo tick: %+v", w)
	}

	clock.Advance(10 * time.Second) // k=2: gracia b, ventana c d a, prefetch b
	w = waitWindow(t, events)
	if w.MediaSequence != 2 {
		t.Fatalf("tercer tick: %+v", w)
	}
	// En k=2 (N=4) el set es n=1..5 → b c d a b: todos los archivos siguen presentes.
	clock.Advance(10 * time.Second) // k=3
	waitWindow(t, events)
	clock.Advance(4 * time.Second) // k=4: set n=3..7 → d a b c
	w = waitWindow(t, events)
	if w.MediaSequence != 4 || !w.Entries[0].Discontinuity {
		t.Fatalf("k=4: %+v", w)
	}

	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run devolvió %v", err)
	}
}

func TestService_EvictaFueraDeVentana(t *testing.T) {
	// Seis segmentos de 10s para que la evicción sea observable.
	tl, _ := NewTimeline([]Segment{
		{"s0.ts", 10 * time.Second}, {"s1.ts", 10 * time.Second}, {"s2.ts", 10 * time.Second},
		{"s3.ts", 10 * time.Second}, {"s4.ts", 10 * time.Second}, {"s5.ts", 10 * time.Second},
	})
	clock := newFakeClock(time.Unix(0, 0))
	svc := NewService(tl, memLoader(nil), clock, quietLogger())
	events, cancel := svc.Subscribe()
	defer cancel()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go svc.Run(ctx)

	waitWindow(t, events) // k=0: s0..s3
	if _, ok := svc.Segment("s4.ts"); ok {
		t.Fatal("s4 no debía estar aún")
	}
	clock.Advance(10 * time.Second)
	waitWindow(t, events) // k=1: s0..s4
	clock.Advance(10 * time.Second)
	waitWindow(t, events) // k=2: s1..s5 → s0 evictado
	if _, ok := svc.Segment("s0.ts"); ok {
		t.Fatal("s0 debía salir de la caché en k=2")
	}
	if _, ok := svc.Segment("s1.ts"); !ok {
		t.Fatal("s1 (gracia) debía seguir disponible")
	}
}

func TestService_FallaEnPrimerTick(t *testing.T) {
	tl := testTimeline(t)
	svc := NewService(tl, memLoader(map[string]error{"b.ts": errors.New("disco")}), newFakeClock(time.Unix(0, 0)), quietLogger())
	err := svc.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "b.ts") {
		t.Fatalf("got %v", err)
	}
}

func TestService_SuscriptorLentoNoBloquea(t *testing.T) {
	tl := testTimeline(t)
	clock := newFakeClock(time.Unix(0, 0))
	svc := NewService(tl, memLoader(nil), clock, quietLogger())
	slow, cancelSlow := svc.Subscribe() // nunca lee
	defer cancelSlow()
	fast, cancelFast := svc.Subscribe()
	defer cancelFast()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go svc.Run(ctx)
	for i := 0; i < 3; i++ {
		waitWindow(t, fast)
		clock.Advance(10 * time.Second)
	}
	if len(slow) != 1 {
		t.Fatalf("el canal lento debía conservar solo un evento, tiene %d", len(slow))
	}
}
