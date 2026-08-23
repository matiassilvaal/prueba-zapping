package stream

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseManifest_Valido(t *testing.T) {
	src := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:10.000000,
segment0.ts
#EXTINF:10.000000,
segment1.ts

#EXTINF:4.566667,
segment2.ts
#EXT-X-ENDLIST
`
	segs, err := ParseManifest(strings.NewReader(src))
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	want := []Segment{
		{Name: "segment0.ts", Duration: 10 * time.Second},
		{Name: "segment1.ts", Duration: 10 * time.Second},
		{Name: "segment2.ts", Duration: 4566667 * time.Microsecond},
	}
	if len(segs) != len(want) {
		t.Fatalf("cantidad: got %d, want %d", len(segs), len(want))
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Errorf("segmento %d: got %+v, want %+v", i, segs[i], want[i])
		}
	}
}

func TestParseManifest_Errores(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want error // nil => solo se exige error != nil
	}{
		{"menos de tres segmentos", "#EXTINF:10,\na.ts\n#EXTINF:10,\nb.ts\n", ErrTooFewSegments},
		{"duración inválida", "#EXTINF:abc,\na.ts\n#EXTINF:10,\nb.ts\n#EXTINF:10,\nc.ts\n", nil},
		{"duración no positiva", "#EXTINF:0,\na.ts\n#EXTINF:10,\nb.ts\n#EXTINF:10,\nc.ts\n", nil},
		{"nombre con ruta", "#EXTINF:10,\n../a.ts\n#EXTINF:10,\nb.ts\n#EXTINF:10,\nc.ts\n", nil},
		{"segmento sin EXTINF", "a.ts\n#EXTINF:10,\nb.ts\n#EXTINF:10,\nc.ts\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest(strings.NewReader(tc.src))
			if err == nil {
				t.Fatal("se esperaba error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
