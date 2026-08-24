package stream

import (
	"context"
	"errors"
	"fmt"
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
// luego notifica a los suscriptores. Un prefetch fallido no bloquea el tick
//
// @param [Window] w: ventana a publicar
//
// @return [error] si falta un segmento obligatorio (no se publica nada)
func (s *Service) publish(w Window) error {
	required, prefetch := s.timeline.cacheNames(w.MediaSequence)
	set, skipped, err := buildSegmentSet(s.set.Load(), required, prefetch, s.load)
	if err != nil {
		return err
	}
	for _, name := range skipped {
		s.logger.Warn("prefetch fallido; se reintentará en el próximo tick", "segment", name)
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
	if s.logger.Enabled(context.Background(), slog.LevelDebug) {
		// Guardia + helper: no se arma el slice de nombres con Debug apagado,
		// y el log no depende de que WindowSize sea exactamente 3.
		s.logger.Debug("tick publicado", "sequence", w.MediaSequence, "discontinuity_sequence", w.DiscontinuitySequence,
			"window", entryNames(w), "cache_bytes", set.bytes(), "next_tick", w.NextTick)
	}
	s.broadcast(w)
	return nil
}

// Snapshot publicado más reciente
//
// @return [*Snapshot] nil hasta el primer tick
func (s *Service) Snapshot() *Snapshot { return s.snapshot.Load() }

// Disponibilidad del stream para healthchecks: exige un snapshot publicado y
// fresco. Si publish falla de forma persistente el worker conserva el snapshot
// anterior; sin este chequeo el healthcheck quedaría en verde con los players
// congelados. Se toleran 2×target de retraso sobre NextTick antes de declarar
// el estancamiento
//
// @return [error] nil si el stream está publicando con normalidad
func (s *Service) Ready() error {
	snap := s.snapshot.Load()
	if snap == nil {
		return errors.New("stream: todavía no se publicó la primera ventana")
	}
	grace := 2 * time.Duration(s.timeline.TargetDuration()) * time.Second
	if delay := s.clock.Now().Sub(snap.Window.NextTick); delay > grace {
		return fmt.Errorf("stream: estancado: %s sin publicar un tick", delay.Round(time.Second))
	}
	return nil
}

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

// Nombres de los segmentos de una ventana (para logs)
//
// @param [Window] w: ventana
//
// @return [[]string] nombres en orden
func entryNames(w Window) []string {
	names := make([]string, len(w.Entries))
	for i, e := range w.Entries {
		names[i] = e.Name
	}
	return names
}
