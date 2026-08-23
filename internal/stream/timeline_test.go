package stream

import (
	"os"
	"testing"
	"time"
)

// Cuatro segmentos: 10s, 10s, 10s, 4s. Total 34s.
func testTimeline(t *testing.T) *Timeline {
	t.Helper()
	tl, err := NewTimeline([]Segment{
		{"a.ts", 10 * time.Second},
		{"b.ts", 10 * time.Second},
		{"c.ts", 10 * time.Second},
		{"d.ts", 4 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestNewTimeline(t *testing.T) {
	tl := testTimeline(t)
	if tl.Len() != 4 || tl.Total() != 34*time.Second || tl.TargetDuration() != 10 {
		t.Fatalf("len=%d total=%s target=%d", tl.Len(), tl.Total(), tl.TargetDuration())
	}
	if _, err := NewTimeline([]Segment{{"a.ts", time.Second}, {"b.ts", time.Second}}); err == nil {
		t.Fatal("se esperaba error por pocos segmentos")
	}
	if _, err := NewTimeline([]Segment{{"a.ts", 0}, {"b.ts", time.Second}, {"c.ts", time.Second}}); err == nil {
		t.Fatal("se esperaba error por duración no positiva")
	}
	tl2, _ := NewTimeline([]Segment{{"a.ts", 6500 * time.Millisecond}, {"b.ts", time.Second}, {"c.ts", time.Second}})
	if tl2.TargetDuration() != 7 {
		t.Fatalf("target: got %d, want 7", tl2.TargetDuration())
	}
}

func TestWindowAt(t *testing.T) {
	tl := testTimeline(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	type want struct {
		seq, disc uint64
		names     [3]string
		discAt    int // índice de la entrada con Discontinuity, -1 si ninguna
		nextTick  time.Duration
	}
	cases := []struct {
		name    string
		elapsed time.Duration
		want    want
	}{
		{"inicio", 0, want{0, 0, [3]string{"a.ts", "b.ts", "c.ts"}, -1, 10 * time.Second}},
		{"antes del primer tick", 9999 * time.Millisecond, want{0, 0, [3]string{"a.ts", "b.ts", "c.ts"}, -1, 10 * time.Second}},
		{"primer tick", 10 * time.Second, want{1, 0, [3]string{"b.ts", "c.ts", "d.ts"}, -1, 20 * time.Second}},
		{"entra el cruce", 20 * time.Second, want{2, 0, [3]string{"c.ts", "d.ts", "a.ts"}, 2, 30 * time.Second}},
		{"tick corto de 4s", 30 * time.Second, want{3, 0, [3]string{"d.ts", "a.ts", "b.ts"}, 1, 34 * time.Second}},
		{"cruce al frente", 34 * time.Second, want{4, 0, [3]string{"a.ts", "b.ts", "c.ts"}, 0, 44 * time.Second}},
		{"sale el tag de discontinuidad", 44 * time.Second, want{5, 1, [3]string{"b.ts", "c.ts", "d.ts"}, -1, 54 * time.Second}},
		{"dos vueltas", 68 * time.Second, want{8, 1, [3]string{"a.ts", "b.ts", "c.ts"}, 0, 78 * time.Second}},
		{"segunda discontinuidad removida", 78 * time.Second, want{9, 2, [3]string{"b.ts", "c.ts", "d.ts"}, -1, 88 * time.Second}},
		{"reloj antes del epoch", -5 * time.Second, want{0, 0, [3]string{"a.ts", "b.ts", "c.ts"}, -1, 10 * time.Second}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := tl.WindowAt(epoch, epoch.Add(tc.elapsed))
			if w.MediaSequence != tc.want.seq {
				t.Errorf("MediaSequence: got %d, want %d", w.MediaSequence, tc.want.seq)
			}
			if w.DiscontinuitySequence != tc.want.disc {
				t.Errorf("DiscontinuitySequence: got %d, want %d", w.DiscontinuitySequence, tc.want.disc)
			}
			if len(w.Entries) != WindowSize {
				t.Fatalf("entries: got %d", len(w.Entries))
			}
			for i, e := range w.Entries {
				if e.Name != tc.want.names[i] {
					t.Errorf("entry %d: got %s, want %s", i, e.Name, tc.want.names[i])
				}
				if e.Discontinuity != (i == tc.want.discAt) {
					t.Errorf("entry %d discontinuity: got %v", i, e.Discontinuity)
				}
			}
			if got := w.NextTick.Sub(epoch); got != tc.want.nextTick {
				t.Errorf("NextTick: got %s, want %s", got, tc.want.nextTick)
			}
		})
	}
}

func TestWindowAt_ManifiestoReal(t *testing.T) {
	f, err := os.Open("../../segments/segment.m3u8")
	if err != nil {
		t.Skip("manifiesto real no disponible:", err)
	}
	defer f.Close()
	segs, err := ParseManifest(f)
	if err != nil {
		t.Fatal(err)
	}
	tl, err := NewTimeline(segs)
	if err != nil {
		t.Fatal(err)
	}
	epoch := time.Unix(0, 0)
	w := tl.WindowAt(epoch, epoch.Add(630*time.Second))
	if w.MediaSequence != 63 || w.Entries[0].Name != "segment63.ts" || w.Entries[1].Name != "segment0.ts" || !w.Entries[1].Discontinuity {
		t.Fatalf("ventana inesperada: %+v", w)
	}
	if got := w.NextTick.Sub(epoch); got != 634566667*time.Microsecond {
		t.Fatalf("NextTick: got %s", got)
	}
}
