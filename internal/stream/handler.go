package stream

import (
	"bytes"
	"net/http"
	"strconv"
	"time"
)

// writeTimeout limita cuánto puede tardar la escritura de una respuesta del
// stream. El servidor corre con WriteTimeout 0 (las conexiones SSE son de
// larga duración), así que sin este deadline por respuesta un cliente que lee
// a goteo retendría la conexión y su goroutine indefinidamente.
const writeTimeout = 30 * time.Second

type handler struct {
	svc *Service
}

// Construye el handler HTTP del stream con rutas relativas: GET /playlist.m3u8
// y GET /{name}. El llamador lo monta bajo el prefijo y la autenticación que quiera
//
// @param [*Service] s: servicio de streaming
//
// @return [http.Handler] mux con las dos rutas
func NewHandler(s *Service) http.Handler {
	h := &handler{svc: s}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /playlist.m3u8", h.servePlaylist)
	mux.HandleFunc("GET /{name}", h.serveSegment)
	return mux
}

// Sirve la playlist vigente con ETag por secuencia y sin caché compartida.
// http.ServeContent resuelve If-None-Match (304, incluso en listas), HEAD y
// Range, igual que en los segmentos
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (h *handler) servePlaylist(w http.ResponseWriter, r *http.Request) {
	snap := h.svc.Snapshot()
	if snap == nil {
		http.Error(w, "el stream todavía no está disponible", http.StatusServiceUnavailable)
		return
	}
	setWriteDeadline(w)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", snap.ETag)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(snap.Playlist))
}

// Sirve un segmento desde la caché; fuera de la ventana responde 404
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request con PathValue "name"
func (h *handler) serveSegment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	b, ok := h.svc.Segment(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	setWriteDeadline(w)
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "private, max-age=3600, immutable")
	w.Header().Set("ETag", strconv.Quote(name))
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}

// Fija el write deadline de la respuesta. Best-effort: si el writer no lo
// soporta (p. ej. httptest.ResponseRecorder) se ignora; los middlewares lo
// exponen vía Unwrap
//
// @param [http.ResponseWriter] w: respuesta
func setWriteDeadline(w http.ResponseWriter) {
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(writeTimeout))
}
