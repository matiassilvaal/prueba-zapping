package stream

import (
	"bytes"
	"fmt"
)

// Renderiza una ventana como playlist HLS de medios en vivo (sin EXT-X-ENDLIST)
//
// @param [Window] w: ventana a serializar
// @param [int] targetDuration: valor de EXT-X-TARGETDURATION en segundos
//
// @return [[]byte] contenido m3u8
func RenderPlaylist(w Window, targetDuration int) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-DISCONTINUITY-SEQUENCE:%d\n",
		targetDuration, w.MediaSequence, w.DiscontinuitySequence)
	for _, e := range w.Entries {
		if e.Discontinuity {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", e.Duration.Seconds(), e.Name)
	}
	return b.Bytes()
}
