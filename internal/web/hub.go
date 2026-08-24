package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"prueba-zapping/internal/stream"
)

const (
	clientBuffer      = 4
	keepaliveInterval = 15 * time.Second
)

type segmentEvent struct {
	Name          string  `json:"name"`
	Duration      float64 `json:"duration"`
	Discontinuity bool    `json:"discontinuity"`
}

type windowEvent struct {
	Sequence              uint64         `json:"sequence"`
	DiscontinuitySequence uint64         `json:"discontinuitySequence"`
	Segments              []segmentEvent `json:"segments"`
	NextTickAt            string         `json:"nextTickAt"`
	SecondsToNextTick     float64        `json:"secondsToNextTick"`
	Viewers               int            `json:"viewers"`
}

type viewersEvent struct {
	Viewers int `json:"viewers"`
}

// Hub reparte eventos del stream a los clientes SSE y cuenta espectadores.
type Hub struct {
	logger *slog.Logger

	mu      sync.Mutex
	clients map[chan []byte]struct{}
	last    *stream.Window
}

// Crea el hub
//
// @param [*slog.Logger] logger: logger
//
// @return [*Hub] hub sin clientes
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{logger: logger, clients: make(map[chan []byte]struct{})}
}

// Consume las ventanas publicadas por el stream y las reenvía a los clientes
//
// @param [context.Context] ctx: cancelación
// @param [<-chan stream.Window] events: canal de stream.Service.Subscribe
func (h *Hub) Run(ctx context.Context, events <-chan stream.Window) {
	for {
		select {
		case <-ctx.Done():
			return
		case w := <-events:
			h.mu.Lock()
			h.last = &w
			msg := formatEvent("window", h.windowEventLocked(w))
			h.broadcastLocked(msg)
			h.mu.Unlock()
		}
	}
}

// Cantidad de clientes SSE conectados
//
// @return [int] espectadores
func (h *Hub) Viewers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Última ventana recibida (para tests)
//
// @return [*stream.Window] nil si aún no llegó ninguna
func (h *Hub) lastWindow() *stream.Window {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

// Atiende una conexión SSE hasta que el cliente se desconecte
//
// @param [http.ResponseWriter] w: respuesta (debe soportar http.Flusher)
// @param [*http.Request] r: request
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE no soportado", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, initial := h.add()
	defer h.remove(ch)
	if initial != nil {
		w.Write(initial)
	}
	flusher.Flush()

	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// Registra un cliente y avisa a todos el nuevo conteo de espectadores
//
// @return [chan []byte] canal del cliente
// @return [[]byte] evento window inicial (nil si aún no hay ventana)
func (h *Hub) add() (chan []byte, []byte) {
	ch := make(chan []byte, clientBuffer)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[ch] = struct{}{}
	var initial []byte
	if h.last != nil {
		initial = formatEvent("window", h.windowEventLocked(*h.last))
	}
	h.broadcastLocked(formatEvent("viewers", viewersEvent{Viewers: len(h.clients)}))
	return ch, initial
}

// Da de baja un cliente y avisa el nuevo conteo
//
// @param [chan []byte] ch: canal del cliente
func (h *Hub) remove(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
	h.broadcastLocked(formatEvent("viewers", viewersEvent{Viewers: len(h.clients)}))
}

// Envía un mensaje a todos los clientes; descarta si el buffer está lleno.
// Requiere h.mu tomado
//
// @param [[]byte] msg: evento serializado
func (h *Hub) broadcastLocked(msg []byte) {
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
			h.logger.Debug("cliente SSE lento; evento descartado")
		}
	}
}

// Construye el evento window con el conteo actual. Requiere h.mu tomado
//
// @param [stream.Window] w: ventana
//
// @return [windowEvent] evento serializable
func (h *Hub) windowEventLocked(w stream.Window) windowEvent {
	segs := make([]segmentEvent, len(w.Entries))
	for i, e := range w.Entries {
		segs[i] = segmentEvent{Name: e.Name, Duration: e.Duration.Seconds(), Discontinuity: e.Discontinuity}
	}
	secs := time.Until(w.NextTick).Seconds()
	if secs < 0 {
		secs = 0
	}
	return windowEvent{
		Sequence:              w.MediaSequence,
		DiscontinuitySequence: w.DiscontinuitySequence,
		Segments:              segs,
		NextTickAt:            w.NextTick.UTC().Format(time.RFC3339Nano),
		SecondsToNextTick:     secs,
		Viewers:               len(h.clients),
	}
}

// Serializa un evento SSE ("event: <name>\ndata: <json>\n\n")
//
// @param [string] name: nombre del evento
// @param [any] v: payload serializable a JSON
//
// @return [[]byte] bytes listos para escribir en la conexión
func formatEvent(name string, v any) []byte {
	var b bytes.Buffer
	b.WriteString("event: ")
	b.WriteString(name)
	b.WriteString("\ndata: ")
	if err := json.NewEncoder(&b).Encode(v); err != nil {
		b.WriteString("{}\n")
	}
	b.WriteString("\n")
	return b.Bytes()
}
