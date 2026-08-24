package stream

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCacheNames(t *testing.T) {
	tl := testTimeline(t) // a b c d
	tl6, err := NewTimeline([]Segment{
		{"s0.ts", 10 * time.Second}, {"s1.ts", 10 * time.Second}, {"s2.ts", 10 * time.Second},
		{"s3.ts", 10 * time.Second}, {"s4.ts", 10 * time.Second}, {"s5.ts", 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		tl           *Timeline
		k            uint64
		wantRequired []string
		wantPrefetch []string
	}{
		{tl, 0, []string{"a.ts", "b.ts", "c.ts"}, []string{"d.ts"}},               // sin gracia en k=0; prefetch n=3
		{tl, 1, []string{"a.ts", "b.ts", "c.ts", "d.ts"}, nil},                    // n=0..3; prefetch n=4 duplica a.ts
		{tl, 3, []string{"c.ts", "d.ts", "a.ts", "b.ts"}, nil},                    // n=2..5; prefetch n=6 duplica c.ts
		{tl, 4, []string{"d.ts", "a.ts", "b.ts", "c.ts"}, nil},                    // n=3..6; prefetch n=7 duplica d.ts
		{tl6, 2, []string{"s1.ts", "s2.ts", "s3.ts", "s4.ts"}, []string{"s5.ts"}}, // n=1..4 + prefetch n=5
	}
	for _, tc := range cases {
		required, prefetch := tc.tl.cacheNames(tc.k)
		if !reflect.DeepEqual(required, tc.wantRequired) || !reflect.DeepEqual(prefetch, tc.wantPrefetch) {
			t.Errorf("k=%d: got (%v, %v), want (%v, %v)", tc.k, required, prefetch, tc.wantRequired, tc.wantPrefetch)
		}
	}
}

func TestBuildSegmentSet_ReutilizaYEvicta(t *testing.T) {
	calls := map[string]int{}
	load := func(name string) ([]byte, error) {
		calls[name]++
		return []byte(name), nil
	}
	s1, _, err := buildSegmentSet(nil, []string{"a.ts", "b.ts"}, nil, load)
	if err != nil {
		t.Fatal(err)
	}
	s2, _, err := buildSegmentSet(s1, []string{"b.ts", "c.ts"}, nil, load)
	if err != nil {
		t.Fatal(err)
	}
	if calls["a.ts"] != 1 || calls["b.ts"] != 1 || calls["c.ts"] != 1 {
		t.Fatalf("cargas inesperadas: %v", calls)
	}
	if _, ok := s2.get("a.ts"); ok {
		t.Fatal("a.ts debía quedar fuera del set nuevo")
	}
	if b, ok := s2.get("b.ts"); !ok || string(b) != "b.ts" {
		t.Fatal("b.ts debía reutilizarse")
	}
	if s2.bytes() != len("b.ts")+len("c.ts") {
		t.Fatalf("bytes: got %d", s2.bytes())
	}
}

func TestBuildSegmentSet_PropagaErrorObligatorio(t *testing.T) {
	boom := errors.New("disco roto")
	_, _, err := buildSegmentSet(nil, []string{"a.ts"}, nil, func(string) ([]byte, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestBuildSegmentSet_OpcionalBestEffort(t *testing.T) {
	boom := errors.New("disco roto")
	load := func(name string) ([]byte, error) {
		if name == "c.ts" {
			return nil, boom
		}
		return []byte(name), nil
	}
	set, skipped, err := buildSegmentSet(nil, []string{"a.ts"}, []string{"b.ts", "c.ts"}, load)
	if err != nil {
		t.Fatalf("un opcional fallido no debía impedir el set: %v", err)
	}
	if !reflect.DeepEqual(skipped, []string{"c.ts"}) {
		t.Fatalf("skipped: %v", skipped)
	}
	if _, ok := set.get("b.ts"); !ok {
		t.Fatal("b.ts opcional debía cargarse")
	}
	if _, ok := set.get("c.ts"); ok {
		t.Fatal("c.ts no debía estar en el set")
	}
}

func TestDirLoaderYVerifyFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.ts"), []byte("xyz"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := DirLoader(dir)("x.ts")
	if err != nil || string(b) != "xyz" {
		t.Fatalf("got %q, %v", b, err)
	}
	segs := []Segment{{"x.ts", time.Second}, {"falta.ts", time.Second}}
	err = VerifyFiles(dir, segs)
	if err == nil || !strings.Contains(err.Error(), "falta.ts") {
		t.Fatalf("se esperaba error mencionando falta.ts, got %v", err)
	}
	if err := VerifyFiles(dir, segs[:1]); err != nil {
		t.Fatal(err)
	}
}
