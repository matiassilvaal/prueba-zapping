package stream

import (
	"bytes"
	"net/http"
	"strconv"
	"time"
)

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

// Sirve la playlist vigente con ETag por secuencia y sin caché intermedia
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (h *handler) servePlaylist(w http.ResponseWriter, r *http.Request) {
	snap := h.svc.Snapshot()
	if snap == nil {
		http.Error(w, "el stream todavía no está disponible", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", snap.ETag)
	if r.Header.Get("If-None-Match") == snap.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(snap.Playlist)))
	w.Write(snap.Playlist)
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
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=3600, immutable")
	w.Header().Set("ETag", strconv.Quote(name))
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}
