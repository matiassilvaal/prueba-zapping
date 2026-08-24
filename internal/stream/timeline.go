package stream

import (
	"fmt"
	"slices"
	"sort"
	"time"
)

// Entry es un segmento dentro de la ventana, con su marca de discontinuidad.
type Entry struct {
	Name          string
	Duration      time.Duration
	Discontinuity bool
}

// Window es el estado de la playlist en un instante dado.
type Window struct {
	MediaSequence         uint64
	DiscontinuitySequence uint64
	Entries               []Entry
	NextTick              time.Time
}

// Timeline precalcula la línea de tiempo de una vuelta completa de segmentos.
type Timeline struct {
	segments []Segment
	starts   []time.Duration // inicio de cada segmento dentro de una vuelta
	total    time.Duration
	target   int
}

// Construye la línea de tiempo a partir de los segmentos del manifiesto
//
// @param [[]Segment] segments: segmentos en orden de reproducción
//
// @return [*Timeline] línea de tiempo lista para calcular ventanas
// @return [error] si hay menos de WindowSize segmentos o una duración no positiva
func NewTimeline(segments []Segment) (*Timeline, error) {
	if len(segments) < WindowSize {
		return nil, ErrTooFewSegments
	}
	starts := make([]time.Duration, len(segments))
	var acc, longest time.Duration
	for i, s := range segments {
		if s.Duration <= 0 {
			return nil, fmt.Errorf("stream: segmento %q con duración no positiva", s.Name)
		}
		starts[i] = acc
		acc += s.Duration
		if s.Duration > longest {
			longest = s.Duration
		}
	}
	return &Timeline{
		segments: slices.Clone(segments),
		starts:   starts,
		total:    acc,
		target:   int((longest + time.Second - 1) / time.Second),
	}, nil
}

// Cantidad de segmentos de una vuelta
//
// @return [int] N
func (t *Timeline) Len() int { return len(t.segments) }

// Valor de EXT-X-TARGETDURATION: techo de la duración máxima en segundos
//
// @return [int] segundos
func (t *Timeline) TargetDuration() int { return t.target }

// Duración total de una vuelta completa
//
// @return [time.Duration] suma de todas las duraciones
func (t *Timeline) Total() time.Duration { return t.total }

// Segmento correspondiente a un índice global (se repite cada N)
//
// @param [uint64] n: índice global
//
// @return [Segment] segmento n % N
func (t *Timeline) segment(n uint64) Segment {
	return t.segments[n%uint64(len(t.segments))]
}

// Instante, relativo al epoch, en que el segmento global n entra en la ventana
//
// @param [uint64] n: índice global
//
// @return [time.Duration] (n / N) * total + inicio del segmento n % N
func (t *Timeline) publishAt(n uint64) time.Duration {
	nn := uint64(len(t.segments))
	return time.Duration(n/nn)*t.total + t.starts[n%nn]
}

// Nombres de archivo que deben estar en caché para la secuencia k.
// required: gracia (k-1) y ventana (k..k+2), sin los cuales no se publica.
// prefetch: k+3, que recién se necesita en el tick siguiente (best-effort)
//
// @param [uint64] k: número de secuencia de medios vigente
//
// @return [[]string] required: nombres obligatorios en orden de índice global
// @return [[]string] prefetch: nombre a precargar (vacío si duplica uno obligatorio)
func (t *Timeline) cacheNames(k uint64) (required, prefetch []string) {
	first := k
	if k > 0 {
		first = k - 1
	}
	last := k + WindowSize - 1
	seen := make(map[string]struct{}, last-first+2)
	for n := first; n <= last; n++ {
		name := t.segment(n).Name
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		required = append(required, name)
	}
	if name := t.segment(last + 1).Name; name != "" {
		if _, dup := seen[name]; !dup {
			prefetch = append(prefetch, name)
		}
	}
	return required, prefetch
}

// Calcula la ventana vigente en un instante. Función pura: no guarda estado
//
// @param [time.Time] epoch: instante en que comenzó el stream
// @param [time.Time] now: instante a consultar
//
// @return [Window] secuencia, entradas y próximo tick
func (t *Timeline) WindowAt(epoch, now time.Time) Window {
	elapsed := now.Sub(epoch)
	if elapsed < 0 {
		elapsed = 0
	}
	nn := uint64(len(t.segments))
	laps := uint64(elapsed / t.total)
	rem := elapsed % t.total
	// mayor i con starts[i] <= rem
	i := sort.Search(len(t.starts), func(j int) bool { return t.starts[j] > rem }) - 1
	k := laps*nn + uint64(i)

	w := Window{
		MediaSequence: k,
		Entries:       make([]Entry, WindowSize),
		NextTick:      epoch.Add(t.publishAt(k + 1)),
	}
	if k >= 1 {
		w.DiscontinuitySequence = (k - 1) / nn
	}
	for j := range w.Entries {
		n := k + uint64(j)
		s := t.segment(n)
		w.Entries[j] = Entry{Name: s.Name, Duration: s.Duration, Discontinuity: n > 0 && n%nn == 0}
	}
	return w
}
