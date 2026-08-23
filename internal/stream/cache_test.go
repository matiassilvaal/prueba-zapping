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
	cases := []struct {
		k    uint64
		want []string
	}{
		{0, []string{"a.ts", "b.ts", "c.ts", "d.ts"}}, // sin gracia en k=0; prefetch n=3
		{1, []string{"a.ts", "b.ts", "c.ts", "d.ts"}}, // n=0..4 → a b c d a (dedupe)
		{3, []string{"c.ts", "d.ts", "a.ts", "b.ts"}}, // n=2..6
		{4, []string{"d.ts", "a.ts", "b.ts", "c.ts"}}, // n=3..7
	}
	for _, tc := range cases {
		if got := tl.cacheNames(tc.k); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("k=%d: got %v, want %v", tc.k, got, tc.want)
		}
	}
}

func TestBuildSegmentSet_ReutilizaYEvicta(t *testing.T) {
	calls := map[string]int{}
	load := func(name string) ([]byte, error) {
		calls[name]++
		return []byte(name), nil
	}
	s1, err := buildSegmentSet(nil, []string{"a.ts", "b.ts"}, load)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := buildSegmentSet(s1, []string{"b.ts", "c.ts"}, load)
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

func TestBuildSegmentSet_PropagaError(t *testing.T) {
	boom := errors.New("disco roto")
	_, err := buildSegmentSet(nil, []string{"a.ts"}, func(string) ([]byte, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
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
