// Package stream implementa el generador de livestreaming HLS: parseo del
// manifiesto fuente, cálculo de la ventana con reloj virtual, render de la
// playlist, caché acotada de segmentos y el handler HTTP que los sirve.
// Solo depende de la biblioteca estándar.
package stream

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// WindowSize es la cantidad de segmentos que expone la playlist en vivo.
const WindowSize = 3

// ErrTooFewSegments indica que el manifiesto no alcanza para llenar una ventana.
var ErrTooFewSegments = errors.New("stream: el manifiesto debe tener al menos 3 segmentos")

// Segment describe un archivo de medios y su duración declarada.
type Segment struct {
	Name     string
	Duration time.Duration
}

// Lee un manifiesto HLS de medios (m3u8) y devuelve sus segmentos en orden
//
// @param [io.Reader] r: contenido del manifiesto
//
// @return [[]Segment] segmentos en el orden del manifiesto
// @return [error] ErrTooFewSegments, o error de formato con número de línea
func ParseManifest(r io.Reader) ([]Segment, error) {
	sc := bufio.NewScanner(r)
	var (
		segs        []Segment
		pending     time.Duration
		havePending bool
		line        int
	)
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		switch {
		case text == "":
			continue
		case strings.HasPrefix(text, "#EXTINF:"):
			d, err := parseExtInf(text)
			if err != nil {
				return nil, fmt.Errorf("stream: línea %d: %w", line, err)
			}
			pending, havePending = d, true
		case strings.HasPrefix(text, "#"):
			continue
		default:
			if !havePending {
				return nil, fmt.Errorf("stream: línea %d: segmento %q sin #EXTINF previo", line, text)
			}
			if err := validateName(text); err != nil {
				return nil, fmt.Errorf("stream: línea %d: %w", line, err)
			}
			segs = append(segs, Segment{Name: text, Duration: pending})
			havePending = false
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("stream: leer manifiesto: %w", err)
	}
	if len(segs) < WindowSize {
		return nil, ErrTooFewSegments
	}
	return segs, nil
}

// Convierte una línea "#EXTINF:<segundos>,<título>" en duración
//
// @param [string] text: línea completa con el prefijo #EXTINF:
//
// @return [time.Duration] duración declarada
// @return [error] si el número es inválido o no positivo
func parseExtInf(text string) (time.Duration, error) {
	v := strings.TrimPrefix(text, "#EXTINF:")
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSpace(v)
	secs, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("duración inválida %q", v)
	}
	if secs <= 0 {
		return 0, fmt.Errorf("duración no positiva %q", v)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// Rechaza nombres de segmento que puedan escapar del directorio de medios
//
// @param [string] name: nombre de archivo declarado en el manifiesto
//
// @return [error] si contiene separadores de ruta o ".."
func validateName(name string) error {
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("nombre de segmento inválido %q", name)
	}
	return nil
}
