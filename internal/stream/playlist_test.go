package stream

import (
	"testing"
	"time"
)

func TestRenderPlaylist_SinDiscontinuidad(t *testing.T) {
	w := Window{
		MediaSequence: 0,
		Entries: []Entry{
			{Name: "segment0.ts", Duration: 10 * time.Second},
			{Name: "segment1.ts", Duration: 10 * time.Second},
			{Name: "segment2.ts", Duration: 10 * time.Second},
		},
	}
	want := "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-TARGETDURATION:10\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXT-X-DISCONTINUITY-SEQUENCE:0\n" +
		"#EXTINF:10.000000,\nsegment0.ts\n" +
		"#EXTINF:10.000000,\nsegment1.ts\n" +
		"#EXTINF:10.000000,\nsegment2.ts\n"
	if got := string(RenderPlaylist(w, 10)); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderPlaylist_ConDiscontinuidad(t *testing.T) {
	w := Window{
		MediaSequence:         127,
		DiscontinuitySequence: 1,
		Entries: []Entry{
			{Name: "segment63.ts", Duration: 4566667 * time.Microsecond},
			{Name: "segment0.ts", Duration: 10 * time.Second, Discontinuity: true},
			{Name: "segment1.ts", Duration: 10 * time.Second},
		},
	}
	want := "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-TARGETDURATION:10\n" +
		"#EXT-X-MEDIA-SEQUENCE:127\n" +
		"#EXT-X-DISCONTINUITY-SEQUENCE:1\n" +
		"#EXTINF:4.566667,\nsegment63.ts\n" +
		"#EXT-X-DISCONTINUITY\n" +
		"#EXTINF:10.000000,\nsegment0.ts\n" +
		"#EXTINF:10.000000,\nsegment1.ts\n"
	if got := string(RenderPlaylist(w, 10)); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
