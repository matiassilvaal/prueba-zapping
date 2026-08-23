package stream

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Clock abstrae el tiempo para poder simularlo en tests.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

// Hora actual del sistema
//
// @return [time.Time] time.Now()
func (realClock) Now() time.Time { return time.Now() }

// Canal que recibe un valor cuando transcurre d
//
// @param [time.Duration] d: espera
//
// @return [<-chan time.Time] time.After(d)
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Reloj real del sistema
//
// @return [Clock] implementación sobre el paquete time
func RealClock() Clock { return realClock{} }

// Snapshot es el estado publicado de la playlist: inmutable una vez creado.
type Snapshot struct {
	Window   Window
	Playlist []byte
	ETag     string
}

// Service es el worker que genera el livestreaming. No conoce a los clientes:
// publica snapshots atómicos que los handlers leen sin bloqueo.
type Service struct {
	timeline *Timeline
	load     SegmentLoader
	clock    Clock
	logger   *slog.Logger

	snapshot atomic.Pointer[Snapshot]
	set      atomic.Pointer[segmentSet]

	mu   sync.Mutex
	subs map[chan Window]struct{}
}

// Crea el servicio de streaming
//
// @param [*Timeline] tl: línea de tiempo de los segmentos
// @param [SegmentLoader] load: origen de los bytes de cada segmento
// @param [Clock] clock: reloj (RealClock en producción)
// @param [*slog.Logger] logger: logger estructurado
//
// @return [*Service] servicio listo para Run
func NewService(tl *Timeline, load SegmentLoader, clock Clock, logger *slog.Logger) *Service {
	return &Service{
		timeline: tl,
		load:     load,
		clock:    clock,
		logger:   logger,
		subs:     make(map[chan Window]struct{}),
	}
}

// Ejecuta el ciclo del worker hasta que se cancele el contexto. El epoch del
// stream es el instante de la llamada
//
// @param [context.Context] ctx: cancelación
//
// @return [error] error de carga en el primer tick, o ctx.Err() al cancelar
func (s *Service) Run(ctx context.Context) error {
	epoch := s.clock.Now()
	s.logger.Info("stream iniciado", "segments", s.timeline.Len(), "total", s.timeline.Total(), "epoch", epoch)
	for {
		w := s.timeline.WindowAt(epoch, s.clock.Now())
		if err := s.publish(w); err != nil {
			if s.snapshot.Load() == nil {
				return err
			}
			s.logger.Error("no se pudo publicar el tick; se conserva el snapshot anterior", "sequence", w.MediaSequence, "error", err)
		}
		wait := w.NextTick.Sub(s.clock.Now())
		if wait < 0 {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.clock.After(wait):
		}
	}
}

// Construye la caché y el snapshot de una ventana y los publica atómicamente;
// luego notifica a los suscriptores
//
// @param [Window] w: ventana a publicar
//
// @return [error] si falta algún segmento (no se publica nada)
func (s *Service) publish(w Window) error {
	set, err := buildSegmentSet(s.set.Load(), s.timeline.cacheNames(w.MediaSequence), s.load)
	if err != nil {
		return err
	}
	snap := &Snapshot{
		Window:   w,
		Playlist: RenderPlaylist(w, s.timeline.TargetDuration()),
		ETag:     strconv.Quote(strconv.FormatUint(w.MediaSequence, 10)),
	}
	// Primero los segmentos, luego la playlist: ningún cliente ve una playlist
	// cuyos archivos aún no están disponibles.
	s.set.Store(set)
	s.snapshot.Store(snap)
	s.logger.Debug("tick publicado", "sequence", w.MediaSequence, "discontinuity_sequence", w.DiscontinuitySequence,
		"window", []string{w.Entries[0].Name, w.Entries[1].Name, w.Entries[2].Name}, "cache_bytes", set.bytes(), "next_tick", w.NextTick)
	s.broadcast(w)
	return nil
}

// Snapshot publicado más reciente
//
// @return [*Snapshot] nil hasta el primer tick
func (s *Service) Snapshot() *Snapshot { return s.snapshot.Load() }

// Bytes de un segmento si pertenece a la ventana vigente (más gracia y prefetch)
//
// @param [string] name: nombre de archivo
//
// @return [[]byte] contenido
// @return [bool] false si no está disponible
func (s *Service) Segment(name string) ([]byte, bool) {
	set := s.set.Load()
	if set == nil {
		return nil, false
	}
	return set.get(name)
}

// Registra un suscriptor que recibe cada ventana publicada. El canal tiene
// buffer 1: si el suscriptor no consume, se descartan eventos sin bloquear
//
// @return [<-chan Window] canal de eventos
// @return [func()] cancelación de la suscripción
func (s *Service) Subscribe() (<-chan Window, func()) {
	ch := make(chan Window, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

// Envía la ventana a todos los suscriptores sin bloquear
//
// @param [Window] w: ventana publicada
func (s *Service) broadcast(w Window) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- w:
		default:
		}
	}
}
