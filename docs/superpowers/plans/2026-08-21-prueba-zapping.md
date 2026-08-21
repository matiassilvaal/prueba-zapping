# Prueba Zapping — Plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Construir en Go un servicio de livestreaming HLS simulado con registro/login de usuarios en PostgreSQL, player web protegido, panel en vivo por SSE y entrega en Docker.

**Architecture:** Un binario (`cmd/server`) compone tres paquetes independientes: `internal/stream` (worker con reloj virtual que publica snapshots inmutables de playlist + caché acotada de segmentos, y su `http.Handler`), `internal/auth` + `internal/db` (usuarios, sesiones con caché TTL, stores Postgres) e `internal/web` (vistas `html/template`, SSE, middlewares). El middleware de sesión envuelve al handler del stream desde fuera; `stream` nunca conoce a los usuarios.

**Tech Stack:** Go 1.26 (stdlib `net/http`, `html/template`, `embed`, `log/slog`), `github.com/jackc/pgx/v5`, `golang.org/x/crypto/bcrypt`, PostgreSQL 17, HLS.js (CDN), Docker multi-stage, Git LFS.

**Spec:** `docs/superpowers/specs/2026-08-21-prueba-zapping-design.md`

## Global Constraints

- Módulo Go: `prueba-zapping`. Versión mínima `go 1.26`.
- Dependencias externas permitidas: `github.com/jackc/pgx/v5`, `golang.org/x/crypto`. Nada más (D-6).
- Identificadores en inglés; comentarios, logs y textos de usuario en español (D-7).
- Toda función (exportada o no) lleva el comentario con formato exacto:
  ```go
  // Qué hace la función
  //
  // @param [Type] var: descripción
  //
  // @return [Type] descripción
  ```
  Si no tiene parámetros o retorno, se omite el bloque correspondiente.
- Commits: prefijo `feat:`, `fix:`, `test:`, `docs:`, `chore:`, `refactor:`; mensaje en español; un commit = una funcionalidad pequeña (D-18).
- Flujo por tarea (D-18): test que falla → implementación mínima → tests en verde con `go test -race ./...` → `go vet ./...` → `gofmt -l .` vacío → commit → **detenerse y esperar revisión y aprobación mutua**.
- `internal/stream` solo importa stdlib. `internal/auth` no importa `web` ni `db`. `internal/web` no importa `db`.
- Ventana de 3 segmentos (`WindowSize = 3`), `EXT-X-DISCONTINUITY` solo en el cruce fin → inicio, `EXT-X-MEDIA-SEQUENCE` nunca se reinicia (D-1, D-9, D-10).
- Sin fallback a disco en el path HTTP: segmento fuera del set → `404` (D-13 C2-bis).
- Autor de los commits: Matías (`git config user.name "Matias Silva"`, `git config user.email "matiassilvaalvarez07@gmail.com"`, configurado en la Tarea 1).
- Cada decisión/problema nuevo durante la ejecución se registra en `docs/DECISIONES.md` (D-n / P-n / Q-n) en el mismo commit que lo origina.

---

## Estructura de archivos

| Archivo | Responsabilidad |
|---|---|
| `go.mod`, `go.sum` | módulo y dependencias |
| `Makefile` | atajos `fmt`, `vet`, `test`, `run`, `docker-build`, `docker-save` |
| `.gitattributes` | `eol=lf` y tracking LFS de `segments/*.ts` |
| `cmd/server/main.go` | composición y ciclo de vida del proceso |
| `internal/config/config.go` | lectura y validación de variables de entorno |
| `internal/stream/manifest.go` | `Segment`, `ParseManifest` |
| `internal/stream/timeline.go` | `Timeline`, `Window`, `Entry`, `WindowAt`, `cacheNames` |
| `internal/stream/playlist.go` | `RenderPlaylist` |
| `internal/stream/cache.go` | `segmentSet`, `buildSegmentSet`, `SegmentLoader`, `DirLoader`, `VerifyFiles` |
| `internal/stream/service.go` | `Clock`, `Snapshot`, `Service` (worker, suscripciones) |
| `internal/stream/handler.go` | `NewHandler`: `GET /playlist.m3u8`, `GET /{name}` |
| `internal/auth/errors.go` | errores de dominio |
| `internal/auth/user.go` | `User`, `UserStore`, validación, bcrypt |
| `internal/auth/session.go` | `Session`, `SessionStore`, tokens, `SessionCache` |
| `internal/auth/service.go` | `Service`: `Register`, `Login`, `Logout`, `Authenticate`, `DeleteExpired` |
| `internal/auth/memory.go` | stores en memoria (para tests de otros paquetes) |
| `internal/auth/middleware.go` | cookie, `RequireSession`, `UserID(ctx)` |
| `internal/db/db.go` | `Connect` (pgxpool) |
| `internal/db/migrate.go` | `Migrate` con tabla `schema_migrations` y advisory lock |
| `internal/db/users.go`, `internal/db/sessions.go` | stores Postgres |
| `migrations/embed.go`, `migrations/0001_init.sql` | SQL embebido |
| `internal/web/templates.go` | embed + render de vistas |
| `internal/web/templates/*.html` | `layout`, `register`, `login`, `player` |
| `internal/web/static/app.css`, `static/player.js` | Neumorphism + lógica del player |
| `internal/web/server.go` | rutas y handlers de páginas |
| `internal/web/hub.go` | hub SSE, espectadores |
| `internal/web/middleware.go` | `Recover`, `Logging` (con `Flush`) |
| `internal/web/e2e_test.go` | flujo completo con `httptest` |
| `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `docker-compose.dev.yml` | empaquetado |
| `README.md`, `INSTALACION.md` | documentación y entrega |

---

### Task 1: Módulo Go, Makefile y estructura base

**Files:**
- Create: `go.mod`, `Makefile`, `.gitattributes`, `cmd/server/main.go`

**Interfaces:**
- Produces: módulo `prueba-zapping`; los demás paquetes importan `prueba-zapping/internal/...`.

- [ ] **Step 1: Configurar autor de git e inicializar módulo**

```bash
git config user.name "Matias Silva"
git config user.email "matiassilvaalvarez07@gmail.com"
go mod init prueba-zapping
```

Verificar que `go.mod` contiene `go 1.26` (ajustar a `go 1.26.0` si `go mod init` escribe la versión con parche: cualquiera de las dos es válida).

- [ ] **Step 2: Crear `.gitattributes` con finales de línea LF**

```
* text=auto eol=lf
*.png binary
*.ts binary
```

- [ ] **Step 3: Crear `Makefile`** (las líneas de receta llevan TAB)

```makefile
GO ?= go
IMAGE ?= prueba-zapping:latest

.PHONY: fmt vet test run docker-build docker-save

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -count=1 ./...

run:
	$(GO) run ./cmd/server

docker-build:
	docker build -t $(IMAGE) .

docker-save: docker-build
	mkdir -p dist
	docker save $(IMAGE) -o dist/prueba-zapping.tar
```

- [ ] **Step 4: Crear `cmd/server/main.go` mínimo** (se reemplaza en la Tarea 21)

```go
// Punto de entrada del servidor de livestreaming. Se completa en la Tarea 21.
package main

import "fmt"

// Imprime el nombre del servicio; marcador hasta que exista la composición real
func main() {
	fmt.Println("prueba-zapping")
}
```

- [ ] **Step 5: Verificar**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: sin salida (build y vet OK, nada sin formatear).

- [ ] **Step 6: Commit**

```bash
git add go.mod Makefile .gitattributes cmd/server/main.go
git commit -m "chore: inicializar módulo Go, Makefile y estructura base"
```

---

### Task 2: Versionar segmentos con Git LFS

**Files:**
- Modify: `.gitattributes`
- Add: `segments/segment.m3u8`, `segments/segment*.ts`

- [ ] **Step 1: Activar LFS y trackear los `.ts`**

```bash
git lfs install --local
git lfs track "segments/*.ts"
```

Verificar que `.gitattributes` ahora incluye la línea `segments/*.ts filter=lfs diff=lfs merge=lfs -text` (mantener las líneas de la Tarea 1).

- [ ] **Step 2: Agregar los archivos**

```bash
git add .gitattributes segments/
git lfs ls-files | wc -l
```

Expected: `64`.

- [ ] **Step 3: Commit**

```bash
git commit -m "chore: versionar segmentos de video con Git LFS"
```

Nota: el push a GitHub requiere `git lfs push --all origin` o un `git push` normal con LFS instalado; cuota gratuita de LFS 1 GB (480 MB usados).

---

### Task 3: `stream.ParseManifest`

**Files:**
- Create: `internal/stream/manifest.go`, `internal/stream/manifest_test.go`

**Interfaces:**
- Produces:
  - `type Segment struct { Name string; Duration time.Duration }`
  - `const WindowSize = 3`
  - `var ErrTooFewSegments error`
  - `func ParseManifest(r io.Reader) ([]Segment, error)`

- [ ] **Step 1: Escribir los tests**

```go
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
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/stream/ -run TestParseManifest -v`
Expected: FAIL (compilación: `ParseManifest` no definido).

- [ ] **Step 3: Implementar `manifest.go`**

```go
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
```

- [ ] **Step 4: Ejecutar tests**

Run: `go test -race ./internal/stream/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/stream/manifest.go internal/stream/manifest_test.go
git commit -m "feat(stream): parsear manifiesto m3u8 de segmentos"
```

---

### Task 4: `stream.Timeline` y `WindowAt` (reloj virtual)

**Files:**
- Create: `internal/stream/timeline.go`, `internal/stream/timeline_test.go`

**Interfaces:**
- Consumes: `Segment`, `WindowSize`.
- Produces:
  - `type Entry struct { Name string; Duration time.Duration; Discontinuity bool }`
  - `type Window struct { MediaSequence, DiscontinuitySequence uint64; Entries []Entry; NextTick time.Time }`
  - `func NewTimeline(segments []Segment) (*Timeline, error)`
  - `func (t *Timeline) Len() int`, `TargetDuration() int`, `Total() time.Duration`
  - `func (t *Timeline) WindowAt(epoch, now time.Time) Window`

- [ ] **Step 1: Escribir los tests**

```go
package stream

import (
	"os"
	"testing"
	"time"
)

// Cuatro segmentos: 10s, 10s, 10s, 4s. Total 34s.
func testTimeline(t *testing.T) *Timeline {
	t.Helper()
	tl, err := NewTimeline([]Segment{
		{"a.ts", 10 * time.Second},
		{"b.ts", 10 * time.Second},
		{"c.ts", 10 * time.Second},
		{"d.ts", 4 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestNewTimeline(t *testing.T) {
	tl := testTimeline(t)
	if tl.Len() != 4 || tl.Total() != 34*time.Second || tl.TargetDuration() != 10 {
		t.Fatalf("len=%d total=%s target=%d", tl.Len(), tl.Total(), tl.TargetDuration())
	}
	if _, err := NewTimeline([]Segment{{"a.ts", time.Second}, {"b.ts", time.Second}}); err == nil {
		t.Fatal("se esperaba error por pocos segmentos")
	}
	if _, err := NewTimeline([]Segment{{"a.ts", 0}, {"b.ts", time.Second}, {"c.ts", time.Second}}); err == nil {
		t.Fatal("se esperaba error por duración no positiva")
	}
	tl2, _ := NewTimeline([]Segment{{"a.ts", 6500 * time.Millisecond}, {"b.ts", time.Second}, {"c.ts", time.Second}})
	if tl2.TargetDuration() != 7 {
		t.Fatalf("target: got %d, want 7", tl2.TargetDuration())
	}
}

func TestWindowAt(t *testing.T) {
	tl := testTimeline(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	type want struct {
		seq, disc uint64
		names     [3]string
		discAt    int // índice de la entrada con Discontinuity, -1 si ninguna
		nextTick  time.Duration
	}
	cases := []struct {
		name    string
		elapsed time.Duration
		want    want
	}{
		{"inicio", 0, want{0, 0, [3]string{"a.ts", "b.ts", "c.ts"}, -1, 10 * time.Second}},
		{"antes del primer tick", 9999 * time.Millisecond, want{0, 0, [3]string{"a.ts", "b.ts", "c.ts"}, -1, 10 * time.Second}},
		{"primer tick", 10 * time.Second, want{1, 0, [3]string{"b.ts", "c.ts", "d.ts"}, -1, 20 * time.Second}},
		{"entra el cruce", 20 * time.Second, want{2, 0, [3]string{"c.ts", "d.ts", "a.ts"}, 2, 30 * time.Second}},
		{"tick corto de 4s", 30 * time.Second, want{3, 0, [3]string{"d.ts", "a.ts", "b.ts"}, 1, 34 * time.Second}},
		{"cruce al frente", 34 * time.Second, want{4, 0, [3]string{"a.ts", "b.ts", "c.ts"}, 0, 44 * time.Second}},
		{"sale el tag de discontinuidad", 44 * time.Second, want{5, 1, [3]string{"b.ts", "c.ts", "d.ts"}, -1, 54 * time.Second}},
		{"dos vueltas", 68 * time.Second, want{8, 1, [3]string{"a.ts", "b.ts", "c.ts"}, 0, 78 * time.Second}},
		{"segunda discontinuidad removida", 78 * time.Second, want{9, 2, [3]string{"b.ts", "c.ts", "d.ts"}, -1, 88 * time.Second}},
		{"reloj antes del epoch", -5 * time.Second, want{0, 0, [3]string{"a.ts", "b.ts", "c.ts"}, -1, 10 * time.Second}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := tl.WindowAt(epoch, epoch.Add(tc.elapsed))
			if w.MediaSequence != tc.want.seq {
				t.Errorf("MediaSequence: got %d, want %d", w.MediaSequence, tc.want.seq)
			}
			if w.DiscontinuitySequence != tc.want.disc {
				t.Errorf("DiscontinuitySequence: got %d, want %d", w.DiscontinuitySequence, tc.want.disc)
			}
			if len(w.Entries) != WindowSize {
				t.Fatalf("entries: got %d", len(w.Entries))
			}
			for i, e := range w.Entries {
				if e.Name != tc.want.names[i] {
					t.Errorf("entry %d: got %s, want %s", i, e.Name, tc.want.names[i])
				}
				if e.Discontinuity != (i == tc.want.discAt) {
					t.Errorf("entry %d discontinuity: got %v", i, e.Discontinuity)
				}
			}
			if got := w.NextTick.Sub(epoch); got != tc.want.nextTick {
				t.Errorf("NextTick: got %s, want %s", got, tc.want.nextTick)
			}
		})
	}
}

func TestWindowAt_ManifiestoReal(t *testing.T) {
	f, err := os.Open("../../segments/segment.m3u8")
	if err != nil {
		t.Skip("manifiesto real no disponible:", err)
	}
	defer f.Close()
	segs, err := ParseManifest(f)
	if err != nil {
		t.Fatal(err)
	}
	tl, err := NewTimeline(segs)
	if err != nil {
		t.Fatal(err)
	}
	epoch := time.Unix(0, 0)
	w := tl.WindowAt(epoch, epoch.Add(630*time.Second))
	if w.MediaSequence != 63 || w.Entries[0].Name != "segment63.ts" || w.Entries[1].Name != "segment0.ts" || !w.Entries[1].Discontinuity {
		t.Fatalf("ventana inesperada: %+v", w)
	}
	if got := w.NextTick.Sub(epoch); got != 634566667*time.Microsecond {
		t.Fatalf("NextTick: got %s", got)
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/stream/ -run 'TestNewTimeline|TestWindowAt' -v`
Expected: FAIL (tipos no definidos).

- [ ] **Step 3: Implementar `timeline.go`**

```go
package stream

import (
	"fmt"
	"math"
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
		segments: append([]Segment(nil), segments...),
		starts:   starts,
		total:    acc,
		target:   int(math.Ceil(longest.Seconds())),
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
```

- [ ] **Step 4: Ejecutar tests**

Run: `go test -race ./internal/stream/ -v`
Expected: PASS (incluido `TestWindowAt_ManifiestoReal`).

- [ ] **Step 5: Commit**

```bash
git add internal/stream/timeline.go internal/stream/timeline_test.go
git commit -m "feat(stream): calcular la ventana en vivo con reloj virtual determinista"
```

---

### Task 5: `stream.RenderPlaylist`

**Files:**
- Create: `internal/stream/playlist.go`, `internal/stream/playlist_test.go`

**Interfaces:**
- Consumes: `Window`, `Entry`.
- Produces: `func RenderPlaylist(w Window, targetDuration int) []byte`

- [ ] **Step 1: Escribir los tests**

```go
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
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/stream/ -run TestRenderPlaylist -v`
Expected: FAIL (`RenderPlaylist` no definido).

- [ ] **Step 3: Implementar `playlist.go`**

```go
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
```

- [ ] **Step 4: Ejecutar tests**

Run: `go test -race ./internal/stream/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/stream/playlist.go internal/stream/playlist_test.go
git commit -m "feat(stream): renderizar playlist HLS en vivo con discontinuidades"
```

---

### Task 6: Caché acotada de segmentos

**Files:**
- Create: `internal/stream/cache.go`, `internal/stream/cache_test.go`
- Modify: `internal/stream/timeline.go` (agregar `cacheNames`)

**Interfaces:**
- Consumes: `Timeline`.
- Produces:
  - `type SegmentLoader func(name string) ([]byte, error)`
  - `func DirLoader(dir string) SegmentLoader`
  - `func VerifyFiles(dir string, segments []Segment) error`
  - `func (t *Timeline) cacheNames(k uint64) []string` — nombres de `n ∈ [k-1, k+3]`, sin duplicados
  - `type segmentSet struct{ data map[string][]byte }`, `func buildSegmentSet(prev *segmentSet, names []string, load SegmentLoader) (*segmentSet, error)`, `func (s *segmentSet) get(name string) ([]byte, bool)`, `func (s *segmentSet) bytes() int`

- [ ] **Step 1: Escribir los tests**

```go
package stream

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCacheNames(t *testing.T) {
	tl := testTimeline(t) // a b c d
	cases := []struct {
		k    uint64
		want []string
	}{
		{0, []string{"a.ts", "b.ts", "c.ts", "d.ts"}},          // sin gracia en k=0; prefetch n=3
		{1, []string{"a.ts", "b.ts", "c.ts", "d.ts"}},          // n=0..4 → a b c d a (dedupe)
		{3, []string{"c.ts", "d.ts", "a.ts", "b.ts"}},          // n=2..6
		{4, []string{"d.ts", "a.ts", "b.ts", "c.ts"}},          // n=3..7
	}
	for _, tc := range cases {
		if got := tl.cacheNames(tc.k); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("k=%d: got %v, want %v", tc.k, got, tc.want)
		}
	}
}

func TestBuildSegmentSet_ReutilizaYEvicta(t *testing.T) {
	calls := map[string]int{}
	load := func(name string) ([]byte, error) {
		calls[name]++
		return []byte(name), nil
	}
	s1, err := buildSegmentSet(nil, []string{"a.ts", "b.ts"}, load)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := buildSegmentSet(s1, []string{"b.ts", "c.ts"}, load)
	if err != nil {
		t.Fatal(err)
	}
	if calls["a.ts"] != 1 || calls["b.ts"] != 1 || calls["c.ts"] != 1 {
		t.Fatalf("cargas inesperadas: %v", calls)
	}
	if _, ok := s2.get("a.ts"); ok {
		t.Fatal("a.ts debía quedar fuera del set nuevo")
	}
	if b, ok := s2.get("b.ts"); !ok || string(b) != "b.ts" {
		t.Fatal("b.ts debía reutilizarse")
	}
	if s2.bytes() != len("b.ts")+len("c.ts") {
		t.Fatalf("bytes: got %d", s2.bytes())
	}
}

func TestBuildSegmentSet_PropagaError(t *testing.T) {
	boom := errors.New("disco roto")
	_, err := buildSegmentSet(nil, []string{"a.ts"}, func(string) ([]byte, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestDirLoaderYVerifyFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.ts"), []byte("xyz"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := DirLoader(dir)("x.ts")
	if err != nil || string(b) != "xyz" {
		t.Fatalf("got %q, %v", b, err)
	}
	segs := []Segment{{"x.ts", time.Second}, {"falta.ts", time.Second}}
	err = VerifyFiles(dir, segs)
	if err == nil || !contains(err.Error(), "falta.ts") {
		t.Fatalf("se esperaba error mencionando falta.ts, got %v", err)
	}
	if err := VerifyFiles(dir, segs[:1]); err != nil {
		t.Fatal(err)
	}
}

// contains evita importar strings solo para esto en el test
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/stream/ -run 'TestCacheNames|TestBuildSegmentSet|TestDirLoader' -v`
Expected: FAIL (no definidos).

- [ ] **Step 3: Agregar `cacheNames` a `timeline.go`**

```go
// Nombres de archivo que deben estar en caché para la secuencia k:
// gracia (k-1), ventana (k..k+2) y prefetch (k+3), sin duplicados
//
// @param [uint64] k: número de secuencia de medios vigente
//
// @return [[]string] nombres en orden de índice global
func (t *Timeline) cacheNames(k uint64) []string {
	first := k
	if k > 0 {
		first = k - 1
	}
	last := k + WindowSize // prefetch
	names := make([]string, 0, last-first+1)
	seen := make(map[string]struct{}, last-first+1)
	for n := first; n <= last; n++ {
		name := t.segment(n).Name
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}
```

- [ ] **Step 4: Implementar `cache.go`**

```go
package stream

import (
	"fmt"
	"os"
	"path/filepath"
)

// SegmentLoader obtiene los bytes de un segmento por nombre.
type SegmentLoader func(name string) ([]byte, error)

// Devuelve un loader que lee archivos desde un directorio
//
// @param [string] dir: directorio con los segmentos
//
// @return [SegmentLoader] loader basado en os.ReadFile
func DirLoader(dir string) SegmentLoader {
	return func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, name))
	}
}

// Comprueba que todos los segmentos del manifiesto existen en el directorio
//
// @param [string] dir: directorio con los segmentos
// @param [[]Segment] segments: segmentos declarados
//
// @return [error] primer archivo faltante o ilegible
func VerifyFiles(dir string, segments []Segment) error {
	for _, s := range segments {
		if _, err := os.Stat(filepath.Join(dir, s.Name)); err != nil {
			return fmt.Errorf("stream: segmento %q no disponible: %w", s.Name, err)
		}
	}
	return nil
}

// segmentSet es un conjunto inmutable de segmentos cargados en memoria.
type segmentSet struct {
	data map[string][]byte
}

// Construye un set con exactamente los nombres indicados, reutilizando los
// bytes ya presentes en prev y cargando solo los faltantes
//
// @param [*segmentSet] prev: set anterior (puede ser nil)
// @param [[]string] names: nombres que debe contener el set nuevo
// @param [SegmentLoader] load: origen de los bytes faltantes
//
// @return [*segmentSet] set nuevo
// @return [error] si alguna carga falla (no se publica set parcial)
func buildSegmentSet(prev *segmentSet, names []string, load SegmentLoader) (*segmentSet, error) {
	data := make(map[string][]byte, len(names))
	for _, name := range names {
		if _, ok := data[name]; ok {
			continue
		}
		if prev != nil {
			if b, ok := prev.data[name]; ok {
				data[name] = b
				continue
			}
		}
		b, err := load(name)
		if err != nil {
			return nil, fmt.Errorf("stream: cargar segmento %q: %w", name, err)
		}
		data[name] = b
	}
	return &segmentSet{data: data}, nil
}

// Busca un segmento en el set
//
// @param [string] name: nombre de archivo
//
// @return [[]byte] contenido
// @return [bool] false si no pertenece al set
func (s *segmentSet) get(name string) ([]byte, bool) {
	b, ok := s.data[name]
	return b, ok
}

// Tamaño total en bytes de los segmentos cacheados (para logs)
//
// @return [int] bytes
func (s *segmentSet) bytes() int {
	n := 0
	for _, b := range s.data {
		n += len(b)
	}
	return n
}
```

- [ ] **Step 5: Ejecutar tests**

Run: `go test -race ./internal/stream/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/stream/cache.go internal/stream/cache_test.go internal/stream/timeline.go
git commit -m "feat(stream): caché acotada de segmentos con gracia y prefetch"
```

---

### Task 7: `stream.Service` (worker)

**Files:**
- Create: `internal/stream/service.go`, `internal/stream/service_test.go`, `internal/stream/clock_test.go`

**Interfaces:**
- Consumes: `Timeline.WindowAt`, `cacheNames`, `buildSegmentSet`, `RenderPlaylist`.
- Produces:
  - `type Clock interface { Now() time.Time; After(d time.Duration) <-chan time.Time }`, `func RealClock() Clock`
  - `type Snapshot struct { Window Window; Playlist []byte; ETag string }`
  - `func NewService(tl *Timeline, load SegmentLoader, clock Clock, logger *slog.Logger) *Service`
  - `func (s *Service) Run(ctx context.Context) error`
  - `func (s *Service) Snapshot() *Snapshot` (nil antes del primer tick)
  - `func (s *Service) Segment(name string) ([]byte, bool)`
  - `func (s *Service) Subscribe() (<-chan Window, func())`

- [ ] **Step 1: Escribir el reloj falso (`clock_test.go`)**

```go
package stream

import (
	"sync"
	"time"
)

// fakeClock permite avanzar el tiempo manualmente en los tests.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After dispara de inmediato si el plazo ya pasó (cubre la carrera entre
// Advance y el registro del temporizador).
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	at := c.now.Add(d)
	if !at.After(c.now) {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, fakeWaiter{at: at, ch: ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	remaining := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
}
```

- [ ] **Step 2: Escribir los tests del servicio**

```go
package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func memLoader(fail map[string]error) SegmentLoader {
	return func(name string) ([]byte, error) {
		if err, ok := fail[name]; ok {
			return nil, err
		}
		return []byte("bytes de " + name), nil
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func waitWindow(t *testing.T, ch <-chan Window) Window {
	t.Helper()
	select {
	case w := <-ch:
		return w
	case <-time.After(2 * time.Second):
		t.Fatal("no llegó el tick")
		return Window{}
	}
}

func TestService_PublicaYAvanza(t *testing.T) {
	tl := testTimeline(t) // a b c d (10,10,10,4)
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc := NewService(tl, memLoader(nil), clock, quietLogger())
	if svc.Snapshot() != nil {
		t.Fatal("no debe haber snapshot antes de Run")
	}
	events, cancel := svc.Subscribe()
	defer cancel()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	w := waitWindow(t, events)
	if w.MediaSequence != 0 {
		t.Fatalf("primer tick: seq %d", w.MediaSequence)
	}
	snap := svc.Snapshot()
	if snap == nil || !strings.Contains(string(snap.Playlist), "#EXT-X-MEDIA-SEQUENCE:0\n") || snap.ETag != `"0"` {
		t.Fatalf("snapshot inesperado: %+v", snap)
	}
	for _, name := range []string{"a.ts", "b.ts", "c.ts", "d.ts"} {
		if _, ok := svc.Segment(name); !ok {
			t.Errorf("%s debía estar en caché en k=0", name)
		}
	}
	if _, ok := svc.Segment("zzz.ts"); ok {
		t.Error("segmento desconocido no debe existir")
	}

	clock.Advance(10 * time.Second)
	w = waitWindow(t, events)
	if w.MediaSequence != 1 || svc.Snapshot().ETag != `"1"` {
		t.Fatalf("segundo tick: %+v", w)
	}

	clock.Advance(10 * time.Second) // k=2: gracia b, ventana c d a, prefetch b
	w = waitWindow(t, events)
	if w.MediaSequence != 2 {
		t.Fatalf("tercer tick: %+v", w)
	}
	// En k=2 (N=4) el set es n=1..5 → b c d a b: todos los archivos siguen presentes.
	clock.Advance(10 * time.Second) // k=3
	waitWindow(t, events)
	clock.Advance(4 * time.Second) // k=4: set n=3..7 → d a b c
	w = waitWindow(t, events)
	if w.MediaSequence != 4 || !w.Entries[0].Discontinuity {
		t.Fatalf("k=4: %+v", w)
	}

	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run devolvió %v", err)
	}
}

func TestService_EvictaFueraDeVentana(t *testing.T) {
	// Seis segmentos de 10s para que la evicción sea observable.
	tl, _ := NewTimeline([]Segment{
		{"s0.ts", 10 * time.Second}, {"s1.ts", 10 * time.Second}, {"s2.ts", 10 * time.Second},
		{"s3.ts", 10 * time.Second}, {"s4.ts", 10 * time.Second}, {"s5.ts", 10 * time.Second},
	})
	clock := newFakeClock(time.Unix(0, 0))
	svc := NewService(tl, memLoader(nil), clock, quietLogger())
	events, cancel := svc.Subscribe()
	defer cancel()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go svc.Run(ctx)

	waitWindow(t, events) // k=0: s0..s3
	if _, ok := svc.Segment("s4.ts"); ok {
		t.Fatal("s4 no debía estar aún")
	}
	clock.Advance(10 * time.Second)
	waitWindow(t, events) // k=1: s0..s4
	clock.Advance(10 * time.Second)
	waitWindow(t, events) // k=2: s1..s5 → s0 evictado
	if _, ok := svc.Segment("s0.ts"); ok {
		t.Fatal("s0 debía salir de la caché en k=2")
	}
	if _, ok := svc.Segment("s1.ts"); !ok {
		t.Fatal("s1 (gracia) debía seguir disponible")
	}
}

func TestService_FallaEnPrimerTick(t *testing.T) {
	tl := testTimeline(t)
	svc := NewService(tl, memLoader(map[string]error{"b.ts": errors.New("disco")}), newFakeClock(time.Unix(0, 0)), quietLogger())
	err := svc.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "b.ts") {
		t.Fatalf("got %v", err)
	}
}

func TestService_SuscriptorLentoNoBloquea(t *testing.T) {
	tl := testTimeline(t)
	clock := newFakeClock(time.Unix(0, 0))
	svc := NewService(tl, memLoader(nil), clock, quietLogger())
	slow, cancelSlow := svc.Subscribe() // nunca lee
	defer cancelSlow()
	fast, cancelFast := svc.Subscribe()
	defer cancelFast()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go svc.Run(ctx)
	for i := 0; i < 3; i++ {
		waitWindow(t, fast)
		clock.Advance(10 * time.Second)
	}
	if len(slow) != 1 {
		t.Fatalf("el canal lento debía conservar solo un evento, tiene %d", len(slow))
	}
}
```

- [ ] **Step 3: Ejecutar y ver que falla**

Run: `go test ./internal/stream/ -run TestService -v`
Expected: FAIL (no definidos).

- [ ] **Step 4: Implementar `service.go`**

```go
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
```

- [ ] **Step 5: Ejecutar tests**

Run: `go test -race ./internal/stream/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/stream/service.go internal/stream/service_test.go internal/stream/clock_test.go
git commit -m "feat(stream): worker que publica snapshots atómicos de la playlist"
```

---

### Task 8: Handler HTTP del stream

**Files:**
- Create: `internal/stream/handler.go`, `internal/stream/handler_test.go`

**Interfaces:**
- Consumes: `Service.Snapshot`, `Service.Segment`.
- Produces: `func NewHandler(s *Service) http.Handler` con rutas relativas `GET /playlist.m3u8` y `GET /{name}`.

- [ ] **Step 1: Escribir los tests**

```go
package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func runningService(t *testing.T) *Service {
	t.Helper()
	tl := testTimeline(t)
	svc := NewService(tl, memLoader(nil), newFakeClock(time.Unix(0, 0)), quietLogger())
	events, cancel := svc.Subscribe()
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(func() { stop(); cancel() })
	go svc.Run(ctx)
	waitWindow(t, events)
	return svc
}

func TestHandler_PlaylistNoLista(t *testing.T) {
	svc := NewService(testTimeline(t), memLoader(nil), newFakeClock(time.Unix(0, 0)), quietLogger())
	rec := httptest.NewRecorder()
	NewHandler(svc).ServeHTTP(rec, httptest.NewRequest("GET", "/playlist.m3u8", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHandler_Playlist(t *testing.T) {
	h := NewHandler(runningService(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/playlist.m3u8", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("content-type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control %q", cc)
	}
	if rec.Header().Get("ETag") != `"0"` {
		t.Errorf("etag %q", rec.Header().Get("ETag"))
	}
	req := httptest.NewRequest("GET", "/playlist.m3u8", nil)
	req.Header.Set("If-None-Match", `"0"`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("304 esperado, got %d", rec.Code)
	}
}

func TestHandler_Segmento(t *testing.T) {
	h := NewHandler(runningService(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/a.ts", nil))
	if rec.Code != 200 || rec.Body.String() != "bytes de a.ts" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("content-type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=3600, immutable" {
		t.Errorf("cache-control %q", cc)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/zzz.ts", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("404 esperado, got %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/a.ts", nil)
	req.Header.Set("Range", "bytes=0-4")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "bytes" {
		t.Fatalf("range: status %d body %q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/stream/ -run TestHandler -v`
Expected: FAIL (`NewHandler` no definido).

- [ ] **Step 3: Implementar `handler.go`**

```go
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
```

- [ ] **Step 4: Ejecutar tests**

Run: `go test -race ./internal/stream/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/stream/handler.go internal/stream/handler_test.go
git commit -m "feat(stream): handler HTTP de playlist y segmentos con ETag y caché"
```

---

### Task 9: Configuración por entorno

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Config struct { Port int; DatabaseURL string; DBMaxConns int32; SegmentsDir, SegmentsManifest string; SessionTTL time.Duration; CookieSecure bool; LogLevel slog.Level }`
  - `func Load(lookup func(string) (string, bool)) (Config, error)`
  - `func FromEnv() (Config, error)`

- [ ] **Step 1: Escribir los tests**

```go
package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 || cfg.DBMaxConns != 10 || cfg.SegmentsDir != "/data/segments" ||
		cfg.SegmentsManifest != "segment.m3u8" || cfg.SessionTTL != 24*time.Hour ||
		cfg.CookieSecure || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("defaults inesperados: %+v", cfg)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		"DATABASE_URL": "postgres://x", "PORT": "9000", "DB_MAX_CONNS": "25",
		"SEGMENTS_DIR": "./segments", "SEGMENTS_MANIFEST": "m.m3u8",
		"SESSION_TTL": "2h", "COOKIE_SECURE": "true", "LOG_LEVEL": "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9000 || cfg.DBMaxConns != 25 || cfg.SegmentsDir != "./segments" ||
		cfg.SegmentsManifest != "m.m3u8" || cfg.SessionTTL != 2*time.Hour || !cfg.CookieSecure ||
		cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("overrides no aplicados: %+v", cfg)
	}
}

func TestLoad_Errores(t *testing.T) {
	cases := map[string]map[string]string{
		"falta DATABASE_URL": {},
		"puerto inválido":    {"DATABASE_URL": "x", "PORT": "abc"},
		"ttl inválido":       {"DATABASE_URL": "x", "SESSION_TTL": "mañana"},
		"nivel inválido":     {"DATABASE_URL": "x", "LOG_LEVEL": "loud"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(lookupFrom(env)); err == nil {
				t.Fatal("se esperaba error")
			} else if !strings.Contains(err.Error(), "config:") {
				t.Fatalf("mensaje sin prefijo: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/config/ -v`
Expected: FAIL (paquete sin código).

- [ ] **Step 3: Implementar `config.go`**

```go
// Package config lee la configuración del servicio desde variables de entorno.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config agrupa todos los parámetros del servicio.
type Config struct {
	Port             int
	DatabaseURL      string
	DBMaxConns       int32
	SegmentsDir      string
	SegmentsManifest string
	SessionTTL       time.Duration
	CookieSecure     bool
	LogLevel         slog.Level
}

// Carga la configuración usando una función de búsqueda (inyectable en tests)
//
// @param [func(string) (string, bool)] lookup: equivalente a os.LookupEnv
//
// @return [Config] configuración validada con defaults aplicados
// @return [error] si falta una variable obligatoria o un valor es inválido
func Load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		Port:             8080,
		DBMaxConns:       10,
		SegmentsDir:      "/data/segments",
		SegmentsManifest: "segment.m3u8",
		SessionTTL:       24 * time.Hour,
		LogLevel:         slog.LevelInfo,
	}
	var ok bool
	if cfg.DatabaseURL, ok = lookup("DATABASE_URL"); !ok || cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("config: la variable DATABASE_URL es obligatoria")
	}
	if v, ok := lookup("PORT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 65535 {
			return cfg, fmt.Errorf("config: PORT inválido %q", v)
		}
		cfg.Port = n
	}
	if v, ok := lookup("DB_MAX_CONNS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("config: DB_MAX_CONNS inválido %q", v)
		}
		cfg.DBMaxConns = int32(n)
	}
	if v, ok := lookup("SEGMENTS_DIR"); ok && v != "" {
		cfg.SegmentsDir = v
	}
	if v, ok := lookup("SEGMENTS_MANIFEST"); ok && v != "" {
		cfg.SegmentsManifest = v
	}
	if v, ok := lookup("SESSION_TTL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return cfg, fmt.Errorf("config: SESSION_TTL inválido %q", v)
		}
		cfg.SessionTTL = d
	}
	if v, ok := lookup("COOKIE_SECURE"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("config: COOKIE_SECURE inválido %q", v)
		}
		cfg.CookieSecure = b
	}
	if v, ok := lookup("LOG_LEVEL"); ok {
		var lvl slog.Level
		if err := lvl.UnmarshalText([]byte(v)); err != nil {
			return cfg, fmt.Errorf("config: LOG_LEVEL inválido %q", v)
		}
		cfg.LogLevel = lvl
	}
	return cfg, nil
}

// Carga la configuración desde el entorno del proceso
//
// @return [Config] configuración validada
// @return [error] ver Load
func FromEnv() (Config, error) {
	return Load(os.LookupEnv)
}
```

- [ ] **Step 4: Ejecutar tests**

Run: `go test -race ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat(config): cargar y validar configuración desde variables de entorno"
```

---

### Task 10: `auth` — usuarios, validación y bcrypt

**Files:**
- Create: `internal/auth/errors.go`, `internal/auth/user.go`, `internal/auth/user_test.go`
- Modify: `go.mod` (agrega `golang.org/x/crypto`)

**Interfaces:**
- Produces:
  - `var ErrEmailTaken, ErrNotFound, ErrInvalidCredentials error`
  - `type User struct { ID int64; Name, Email string; PasswordHash []byte; CreatedAt time.Time }`
  - `type UserStore interface { Create(ctx, name, email string, passwordHash []byte) (User, error); FindByEmail(ctx, email string) (User, error) }`
  - `type RegistrationInput struct { Name, Email, Password string }`
  - `type FieldError struct { Field, Message string }`, `type ValidationErrors []FieldError` (implementa `error`, método `ByField() map[string]string`)
  - `func ValidateRegistration(in RegistrationInput) (RegistrationInput, error)` — devuelve el input normalizado
  - `func HashPassword(password string) ([]byte, error)`, `func CheckPassword(hash []byte, password string) bool`
  - `var bcryptCost = 12` (los tests lo bajan a `bcrypt.MinCost`)

- [ ] **Step 1: Agregar dependencia**

Run: `go get golang.org/x/crypto@latest`

- [ ] **Step 2: Escribir los tests**

```go
package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func init() { bcryptCost = bcrypt.MinCost }

func TestValidateRegistration_Normaliza(t *testing.T) {
	in, err := ValidateRegistration(RegistrationInput{Name: "  Matías ", Email: " Ana@Example.COM ", Password: "secreto123"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Name != "Matías" || in.Email != "ana@example.com" || in.Password != "secreto123" {
		t.Fatalf("normalización: %+v", in)
	}
}

func TestValidateRegistration_Errores(t *testing.T) {
	cases := []struct {
		name  string
		in    RegistrationInput
		field string
	}{
		{"nombre vacío", RegistrationInput{"", "a@b.co", "12345678"}, "name"},
		{"nombre largo", RegistrationInput{strings.Repeat("x", 101), "a@b.co", "12345678"}, "name"},
		{"email vacío", RegistrationInput{"A", "", "12345678"}, "email"},
		{"email sin arroba", RegistrationInput{"A", "ab.co", "12345678"}, "email"},
		{"email con nombre", RegistrationInput{"A", "Ana <a@b.co>", "12345678"}, "email"},
		{"contraseña corta", RegistrationInput{"A", "a@b.co", "1234567"}, "password"},
		{"contraseña larga", RegistrationInput{"A", "a@b.co", strings.Repeat("x", 73)}, "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateRegistration(tc.in)
			var verr ValidationErrors
			if !errors.As(err, &verr) {
				t.Fatalf("se esperaba ValidationErrors, got %v", err)
			}
			if _, ok := verr.ByField()[tc.field]; !ok {
				t.Fatalf("se esperaba error en %q, got %v", tc.field, verr)
			}
		})
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("secreto123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "secreto123") {
		t.Fatal("la contraseña correcta debía validar")
	}
	if CheckPassword(hash, "otra") {
		t.Fatal("una contraseña incorrecta no debía validar")
	}
}
```

- [ ] **Step 3: Ejecutar y ver que falla**

Run: `go test ./internal/auth/ -v`
Expected: FAIL (no definidos).

- [ ] **Step 4: Implementar `errors.go`**

```go
// Package auth implementa registro, login y sesiones server-side. Define las
// interfaces de persistencia (UserStore, SessionStore) sin depender de ninguna
// base de datos concreta.
package auth

import "errors"

var (
	// ErrEmailTaken indica que ya existe una cuenta con ese email.
	ErrEmailTaken = errors.New("auth: ya existe una cuenta con ese email")
	// ErrNotFound indica que el usuario o la sesión no existen (o expiró).
	ErrNotFound = errors.New("auth: no encontrado")
	// ErrInvalidCredentials indica email o contraseña incorrectos.
	ErrInvalidCredentials = errors.New("auth: credenciales inválidas")
)
```

- [ ] **Step 5: Implementar `user.go`**

```go
package auth

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Límites de validación del registro.
const (
	MaxNameLen     = 100
	MinPasswordLen = 8
	MaxPasswordLen = 72 // límite de bcrypt
)

// bcryptCost es el costo de hash; los tests lo reducen.
var bcryptCost = 12

// User es una cuenta registrada.
type User struct {
	ID           int64
	Name         string
	Email        string
	PasswordHash []byte
	CreatedAt    time.Time
}

// UserStore persiste usuarios.
type UserStore interface {
	Create(ctx context.Context, name, email string, passwordHash []byte) (User, error)
	FindByEmail(ctx context.Context, email string) (User, error)
}

// RegistrationInput son los datos del formulario de registro.
type RegistrationInput struct {
	Name     string
	Email    string
	Password string
}

// FieldError es un error de validación asociado a un campo del formulario.
type FieldError struct {
	Field   string
	Message string
}

// ValidationErrors agrupa errores de validación; implementa error.
type ValidationErrors []FieldError

// Describe todos los errores de validación en una línea
//
// @return [string] mensajes separados por "; "
func (v ValidationErrors) Error() string {
	parts := make([]string, len(v))
	for i, e := range v {
		parts[i] = e.Field + ": " + e.Message
	}
	return "auth: validación: " + strings.Join(parts, "; ")
}

// Indexa los errores por campo para mostrarlos en el formulario
//
// @return [map[string]string] campo → mensaje
func (v ValidationErrors) ByField() map[string]string {
	m := make(map[string]string, len(v))
	for _, e := range v {
		if _, ok := m[e.Field]; !ok {
			m[e.Field] = e.Message
		}
	}
	return m
}

// Valida y normaliza los datos de registro (recorta espacios, email en minúsculas)
//
// @param [RegistrationInput] in: datos crudos del formulario
//
// @return [RegistrationInput] datos normalizados
// @return [error] ValidationErrors si algún campo es inválido
func ValidateRegistration(in RegistrationInput) (RegistrationInput, error) {
	var errs ValidationErrors
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		errs = append(errs, FieldError{"name", "El nombre es obligatorio"})
	case n > MaxNameLen:
		errs = append(errs, FieldError{"name", fmt.Sprintf("El nombre no puede superar %d caracteres", MaxNameLen)})
	}
	if in.Email == "" {
		errs = append(errs, FieldError{"email", "El email es obligatorio"})
	} else if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
		errs = append(errs, FieldError{"email", "El email no es válido"})
	}
	switch n := len(in.Password); {
	case n < MinPasswordLen:
		errs = append(errs, FieldError{"password", fmt.Sprintf("La contraseña debe tener al menos %d caracteres", MinPasswordLen)})
	case n > MaxPasswordLen:
		errs = append(errs, FieldError{"password", fmt.Sprintf("La contraseña no puede superar %d caracteres", MaxPasswordLen)})
	}
	if len(errs) > 0 {
		return in, errs
	}
	return in, nil
}

// Genera el hash bcrypt de una contraseña
//
// @param [string] password: contraseña en claro
//
// @return [[]byte] hash
// @return [error] si bcrypt falla
func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
}

// Comprueba una contraseña contra su hash
//
// @param [[]byte] hash: hash almacenado
// @param [string] password: contraseña en claro
//
// @return [bool] true si coincide
func CheckPassword(hash []byte, password string) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}
```

- [ ] **Step 6: Ejecutar tests**

Run: `go test -race ./internal/auth/ -v && go mod tidy && git diff --stat go.mod go.sum`
Expected: PASS; `go.mod` con `golang.org/x/crypto`.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/auth/errors.go internal/auth/user.go internal/auth/user_test.go
git commit -m "feat(auth): modelo de usuario, validación de registro y hash bcrypt"
```

---

### Task 11: `auth` — tokens y caché de sesiones

**Files:**
- Create: `internal/auth/session.go`, `internal/auth/session_test.go`

**Interfaces:**
- Produces:
  - `type Session struct { TokenHash []byte; UserID int64; ExpiresAt time.Time }`
  - `type SessionStore interface { Create(ctx, s Session) error; Get(ctx, tokenHash []byte) (Session, error); Delete(ctx, tokenHash []byte) error; DeleteExpired(ctx, now time.Time) (int64, error) }`
  - `func NewToken() (string, error)`, `func HashToken(token string) []byte`
  - `func NewSessionCache(ttl time.Duration, maxEntries int) *SessionCache` con métodos `Get(hash []byte) (int64, bool)`, `Put(hash []byte, userID int64, expiresAt time.Time)`, `Delete(hash []byte)`, `Sweep() int`, `Len() int`; campo `now func() time.Time` inyectable.

- [ ] **Step 1: Escribir los tests**

```go
package auth

import (
	"testing"
	"time"
)

func TestNewTokenYHash(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()
	if a == b || len(a) < 40 {
		t.Fatalf("tokens débiles: %q %q", a, b)
	}
	if h := HashToken(a); len(h) != 32 || string(h) == string(HashToken(b)) {
		t.Fatal("hash inesperado")
	}
}

func TestSessionCache(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewSessionCache(30*time.Second, 2)
	c.now = func() time.Time { return now }
	h1, h2, h3 := HashToken("1"), HashToken("2"), HashToken("3")

	if _, ok := c.Get(h1); ok {
		t.Fatal("miss esperado")
	}
	c.Put(h1, 7, now.Add(time.Hour))
	if id, ok := c.Get(h1); !ok || id != 7 {
		t.Fatal("hit esperado")
	}
	now = now.Add(31 * time.Second)
	if _, ok := c.Get(h1); ok {
		t.Fatal("la entrada debía vencer por TTL de caché")
	}
	c.Put(h1, 7, now.Add(5*time.Second))
	now = now.Add(6 * time.Second)
	if _, ok := c.Get(h1); ok {
		t.Fatal("la entrada debía vencer por expiración de la sesión")
	}

	c.Put(h1, 1, now.Add(time.Hour))
	c.Put(h2, 2, now.Add(time.Hour))
	c.Put(h3, 3, now.Add(time.Hour)) // supera el máximo: no se cachea
	if _, ok := c.Get(h3); ok || c.Len() != 2 {
		t.Fatal("no debía superar el máximo de entradas")
	}
	c.Delete(h1)
	if _, ok := c.Get(h1); ok {
		t.Fatal("borrado esperado")
	}
	now = now.Add(time.Minute)
	if n := c.Sweep(); n != 1 || c.Len() != 0 {
		t.Fatalf("sweep: n=%d len=%d", n, c.Len())
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/auth/ -run 'TestNewToken|TestSessionCache' -v`
Expected: FAIL.

- [ ] **Step 3: Implementar `session.go`**

```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// Session es una sesión server-side; solo se guarda el hash del token.
type Session struct {
	TokenHash []byte
	UserID    int64
	ExpiresAt time.Time
}

// SessionStore persiste sesiones. Get no devuelve sesiones expiradas.
type SessionStore interface {
	Create(ctx context.Context, s Session) error
	Get(ctx context.Context, tokenHash []byte) (Session, error)
	Delete(ctx context.Context, tokenHash []byte) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// Genera un token de sesión aleatorio (32 bytes, base64url sin padding)
//
// @return [string] token para la cookie
// @return [error] si el generador aleatorio falla
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Deriva el hash SHA-256 de un token; es lo único que se persiste
//
// @param [string] token: token de la cookie
//
// @return [[]byte] 32 bytes
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

type cacheEntry struct {
	userID    int64
	expiresAt time.Time
	cachedAt  time.Time
}

// SessionCache evita consultar la base de datos en cada request del stream.
// Acotada por TTL y por cantidad de entradas.
type SessionCache struct {
	mu         sync.RWMutex
	entries    map[string]cacheEntry
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
}

// Crea una caché de sesiones
//
// @param [time.Duration] ttl: tiempo máximo que una entrada se considera válida sin reconsultar la DB
// @param [int] maxEntries: tope de entradas; al alcanzarlo, Put no cachea nuevas
//
// @return [*SessionCache] caché vacía
func NewSessionCache(ttl time.Duration, maxEntries int) *SessionCache {
	return &SessionCache{entries: make(map[string]cacheEntry), ttl: ttl, maxEntries: maxEntries, now: time.Now}
}

// Busca una sesión vigente en caché
//
// @param [[]byte] hash: hash del token
//
// @return [int64] id de usuario
// @return [bool] false si no está, venció el TTL de caché o expiró la sesión
func (c *SessionCache) Get(hash []byte) (int64, bool) {
	c.mu.RLock()
	e, ok := c.entries[string(hash)]
	c.mu.RUnlock()
	if !ok {
		return 0, false
	}
	now := c.now()
	if now.Sub(e.cachedAt) > c.ttl || !now.Before(e.expiresAt) {
		c.Delete(hash)
		return 0, false
	}
	return e.userID, true
}

// Guarda o refresca una sesión en caché
//
// @param [[]byte] hash: hash del token
// @param [int64] userID: usuario dueño de la sesión
// @param [time.Time] expiresAt: expiración de la sesión
func (c *SessionCache) Put(hash []byte, userID int64, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := string(hash)
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		return
	}
	c.entries[key] = cacheEntry{userID: userID, expiresAt: expiresAt, cachedAt: c.now()}
}

// Elimina una sesión de la caché (logout)
//
// @param [[]byte] hash: hash del token
func (c *SessionCache) Delete(hash []byte) {
	c.mu.Lock()
	delete(c.entries, string(hash))
	c.mu.Unlock()
}

// Elimina las entradas vencidas
//
// @return [int] cantidad eliminada
func (c *SessionCache) Sweep() int {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for k, e := range c.entries {
		if now.Sub(e.cachedAt) > c.ttl || !now.Before(e.expiresAt) {
			delete(c.entries, k)
			n++
		}
	}
	return n
}

// Cantidad de entradas en caché
//
// @return [int] entradas
func (c *SessionCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
```

- [ ] **Step 4: Ejecutar tests**

Run: `go test -race ./internal/auth/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/session.go internal/auth/session_test.go
git commit -m "feat(auth): tokens de sesión y caché en memoria con TTL"
```

---

### Task 12: `auth.Service` y stores en memoria

**Files:**
- Create: `internal/auth/memory.go`, `internal/auth/service.go`, `internal/auth/service_test.go`

**Interfaces:**
- Consumes: `UserStore`, `SessionStore`, `SessionCache`, `ValidateRegistration`, `HashPassword`, `CheckPassword`, `NewToken`, `HashToken`.
- Produces:
  - `func NewMemoryUserStore() *MemoryUserStore`, `func NewMemorySessionStore() *MemorySessionStore` (implementan las interfaces)
  - `func NewService(users UserStore, sessions SessionStore, ttl time.Duration) *Service`
  - `func (s *Service) Register(ctx, in RegistrationInput) (User, string, error)` — devuelve usuario y token
  - `func (s *Service) Login(ctx, email, password string) (User, string, error)`
  - `func (s *Service) Logout(ctx, token string) error`
  - `func (s *Service) Authenticate(ctx, token string) (int64, error)` — `ErrNotFound` si inválida
  - `func (s *Service) DeleteExpired(ctx) (int64, error)`
  - `func (s *Service) SweepCache() int`
  - `func (s *Service) TTL() time.Duration`

- [ ] **Step 1: Escribir los tests**

```go
package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestService() *Service {
	return NewService(NewMemoryUserStore(), NewMemorySessionStore(), time.Hour)
}

func TestService_RegistroLoginLogout(t *testing.T) {
	ctx := context.Background()
	svc := newTestService()

	u, token, err := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"})
	if err != nil || u.ID == 0 || token == "" {
		t.Fatalf("registro: %v %+v %q", err, u, token)
	}
	if id, err := svc.Authenticate(ctx, token); err != nil || id != u.ID {
		t.Fatalf("authenticate tras registro: %d %v", id, err)
	}

	_, _, err = svc.Register(ctx, RegistrationInput{"Otra", "ANA@example.com", "secreto123"})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("email duplicado: %v", err)
	}
	_, _, err = svc.Register(ctx, RegistrationInput{"", "x", "1"})
	var verr ValidationErrors
	if !errors.As(err, &verr) {
		t.Fatalf("validación: %v", err)
	}

	if _, _, err := svc.Login(ctx, "ana@example.com", "incorrecta"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("contraseña incorrecta: %v", err)
	}
	if _, _, err := svc.Login(ctx, "nadie@example.com", "secreto123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("usuario inexistente: %v", err)
	}
	u2, token2, err := svc.Login(ctx, " Ana@Example.com ", "secreto123")
	if err != nil || u2.ID != u.ID || token2 == "" || token2 == token {
		t.Fatalf("login: %v", err)
	}

	if err := svc.Logout(ctx, token2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sesión cerrada debía fallar: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "token-inventado"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token inválido: %v", err)
	}
}

func TestService_AuthenticateUsaCache(t *testing.T) {
	ctx := context.Background()
	sessions := NewMemorySessionStore()
	svc := NewService(NewMemoryUserStore(), sessions, time.Hour)
	_, token, err := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"})
	if err != nil {
		t.Fatal(err)
	}
	before := sessions.Gets()
	for i := 0; i < 5; i++ {
		if _, err := svc.Authenticate(ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	if sessions.Gets() != before {
		t.Fatalf("Authenticate consultó el store %d veces con caché caliente", sessions.Gets()-before)
	}
}

func TestService_SesionExpirada(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryUserStore(), NewMemorySessionStore(), time.Minute)
	now := time.Now()
	svc.now = func() time.Time { return now }
	svc.cache.now = svc.now
	_, token, _ := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"})
	now = now.Add(2 * time.Minute)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sesión expirada: %v", err)
	}
	if n, _ := svc.DeleteExpired(ctx); n != 1 {
		t.Fatalf("DeleteExpired: %d", n)
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/auth/ -run TestService -v`
Expected: FAIL.

- [ ] **Step 3: Implementar `memory.go`**

```go
package auth

import (
	"context"
	"sync"
	"time"
)

// MemoryUserStore es un UserStore en memoria para tests.
type MemoryUserStore struct {
	mu     sync.Mutex
	byMail map[string]User
	nextID int64
}

// Crea un store de usuarios en memoria
//
// @return [*MemoryUserStore] store vacío
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{byMail: make(map[string]User)}
}

// Crea un usuario; falla con ErrEmailTaken si el email existe
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [string] name: nombre
// @param [string] email: email normalizado
// @param [[]byte] passwordHash: hash bcrypt
//
// @return [User] usuario creado con ID asignado
// @return [error] ErrEmailTaken
func (m *MemoryUserStore) Create(ctx context.Context, name, email string, passwordHash []byte) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byMail[email]; ok {
		return User{}, ErrEmailTaken
	}
	m.nextID++
	u := User{ID: m.nextID, Name: name, Email: email, PasswordHash: passwordHash, CreatedAt: time.Now()}
	m.byMail[email] = u
	return u, nil
}

// Busca un usuario por email
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [string] email: email normalizado
//
// @return [User] usuario
// @return [error] ErrNotFound
func (m *MemoryUserStore) FindByEmail(ctx context.Context, email string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byMail[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

// MemorySessionStore es un SessionStore en memoria para tests; cuenta las lecturas.
type MemorySessionStore struct {
	mu   sync.Mutex
	data map[string]Session
	gets int
}

// Crea un store de sesiones en memoria
//
// @return [*MemorySessionStore] store vacío
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{data: make(map[string]Session)}
}

// Guarda una sesión
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [Session] s: sesión
//
// @return [error] siempre nil
func (m *MemorySessionStore) Create(ctx context.Context, s Session) error {
	m.mu.Lock()
	m.data[string(s.TokenHash)] = s
	m.mu.Unlock()
	return nil
}

// Obtiene una sesión no expirada
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [[]byte] tokenHash: hash del token
//
// @return [Session] sesión
// @return [error] ErrNotFound si no existe o expiró
func (m *MemorySessionStore) Get(ctx context.Context, tokenHash []byte) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	s, ok := m.data[string(tokenHash)]
	if !ok || !time.Now().Before(s.ExpiresAt) {
		return Session{}, ErrNotFound
	}
	return s, nil
}

// Elimina una sesión
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [[]byte] tokenHash: hash del token
//
// @return [error] siempre nil
func (m *MemorySessionStore) Delete(ctx context.Context, tokenHash []byte) error {
	m.mu.Lock()
	delete(m.data, string(tokenHash))
	m.mu.Unlock()
	return nil
}

// Elimina las sesiones expiradas respecto de now
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [time.Time] now: instante de referencia
//
// @return [int64] cantidad eliminada
// @return [error] siempre nil
func (m *MemorySessionStore) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k, s := range m.data {
		if !now.Before(s.ExpiresAt) {
			delete(m.data, k)
			n++
		}
	}
	return n, nil
}

// Cantidad de llamadas a Get (para verificar el uso de caché en tests)
//
// @return [int] lecturas acumuladas
func (m *MemorySessionStore) Gets() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gets
}
```

- [ ] **Step 4: Implementar `service.go`**

```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cacheTTL        = 30 * time.Second
	cacheMaxEntries = 10_000
)

// Service orquesta registro, login, logout y validación de sesiones.
type Service struct {
	users    UserStore
	sessions SessionStore
	cache    *SessionCache
	ttl      time.Duration
	now      func() time.Time
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

// Hash de referencia para igualar el tiempo de respuesta cuando el email no existe
//
// @return [[]byte] hash bcrypt de una contraseña fija
func getDummyHash() []byte {
	dummyHashOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("contraseña-de-relleno"), bcryptCost)
	})
	return dummyHash
}

// Crea el servicio de autenticación
//
// @param [UserStore] users: persistencia de usuarios
// @param [SessionStore] sessions: persistencia de sesiones
// @param [time.Duration] ttl: duración de cada sesión
//
// @return [*Service] servicio
func NewService(users UserStore, sessions SessionStore, ttl time.Duration) *Service {
	return &Service{
		users:    users,
		sessions: sessions,
		cache:    NewSessionCache(cacheTTL, cacheMaxEntries),
		ttl:      ttl,
		now:      time.Now,
	}
}

// Duración configurada de las sesiones
//
// @return [time.Duration] ttl
func (s *Service) TTL() time.Duration { return s.ttl }

// Registra un usuario y abre su sesión
//
// @param [context.Context] ctx: contexto
// @param [RegistrationInput] in: datos del formulario
//
// @return [User] usuario creado
// @return [string] token de sesión para la cookie
// @return [error] ValidationErrors, ErrEmailTaken u otro error del store
func (s *Service) Register(ctx context.Context, in RegistrationInput) (User, string, error) {
	in, err := ValidateRegistration(in)
	if err != nil {
		return User{}, "", err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, "", fmt.Errorf("auth: hash de contraseña: %w", err)
	}
	u, err := s.users.Create(ctx, in.Name, in.Email, hash)
	if err != nil {
		return User{}, "", err
	}
	token, err := s.openSession(ctx, u.ID)
	return u, token, err
}

// Valida credenciales y abre una sesión
//
// @param [context.Context] ctx: contexto
// @param [string] email: email (se normaliza)
// @param [string] password: contraseña en claro
//
// @return [User] usuario autenticado
// @return [string] token de sesión
// @return [error] ErrInvalidCredentials u otro error del store
func (s *Service) Login(ctx context.Context, email, password string) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.users.FindByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrNotFound):
		CheckPassword(getDummyHash(), password) // mismo costo que un login real
		return User{}, "", ErrInvalidCredentials
	case err != nil:
		return User{}, "", err
	}
	if !CheckPassword(u.PasswordHash, password) {
		return User{}, "", ErrInvalidCredentials
	}
	token, err := s.openSession(ctx, u.ID)
	return u, token, err
}

// Cierra una sesión en la persistencia y en la caché
//
// @param [context.Context] ctx: contexto
// @param [string] token: token de la cookie
//
// @return [error] error del store
func (s *Service) Logout(ctx context.Context, token string) error {
	hash := HashToken(token)
	s.cache.Delete(hash)
	return s.sessions.Delete(ctx, hash)
}

// Resuelve el usuario de un token, usando la caché antes que la persistencia
//
// @param [context.Context] ctx: contexto
// @param [string] token: token de la cookie
//
// @return [int64] id de usuario
// @return [error] ErrNotFound si el token no corresponde a una sesión vigente
func (s *Service) Authenticate(ctx context.Context, token string) (int64, error) {
	if token == "" {
		return 0, ErrNotFound
	}
	hash := HashToken(token)
	if id, ok := s.cache.Get(hash); ok {
		return id, nil
	}
	sess, err := s.sessions.Get(ctx, hash)
	if err != nil {
		return 0, err
	}
	if !s.now().Before(sess.ExpiresAt) {
		return 0, ErrNotFound
	}
	s.cache.Put(hash, sess.UserID, sess.ExpiresAt)
	return sess.UserID, nil
}

// Elimina de la persistencia las sesiones expiradas
//
// @param [context.Context] ctx: contexto
//
// @return [int64] cantidad eliminada
// @return [error] error del store
func (s *Service) DeleteExpired(ctx context.Context) (int64, error) {
	return s.sessions.DeleteExpired(ctx, s.now())
}

// Elimina de la caché las entradas vencidas
//
// @return [int] cantidad eliminada
func (s *Service) SweepCache() int { return s.cache.Sweep() }

// Crea y persiste una sesión nueva para un usuario
//
// @param [context.Context] ctx: contexto
// @param [int64] userID: usuario
//
// @return [string] token
// @return [error] error del generador o del store
func (s *Service) openSession(ctx context.Context, userID int64) (string, error) {
	token, err := NewToken()
	if err != nil {
		return "", fmt.Errorf("auth: generar token: %w", err)
	}
	sess := Session{TokenHash: HashToken(token), UserID: userID, ExpiresAt: s.now().Add(s.ttl)}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return "", err
	}
	s.cache.Put(sess.TokenHash, userID, sess.ExpiresAt)
	return token, nil
}
```

- [ ] **Step 5: Ejecutar tests**

Run: `go test -race ./internal/auth/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/auth/memory.go internal/auth/service.go internal/auth/service_test.go
git commit -m "feat(auth): servicio de registro, login, logout y autenticación con caché"
```

---

### Task 13: `auth` — cookie y middleware `RequireSession`

**Files:**
- Create: `internal/auth/middleware.go`, `internal/auth/middleware_test.go`

**Interfaces:**
- Consumes: `Service.Authenticate`.
- Produces:
  - `const CookieName = "session"`
  - `type Authenticator interface { Authenticate(ctx, token string) (int64, error) }`
  - `type FailureMode int`; `const ( RedirectToLogin FailureMode = iota; Unauthorized )`
  - `func SetSessionCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool)`
  - `func ClearSessionCookie(w http.ResponseWriter, secure bool)`
  - `func TokenFromRequest(r *http.Request) string`
  - `func RequireSession(a Authenticator, mode FailureMode, next http.Handler) http.Handler`
  - `func UserID(ctx context.Context) (int64, bool)`

- [ ] **Step 1: Escribir los tests**

```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeAuth struct{ valid map[string]int64 }

func (f fakeAuth) Authenticate(_ context.Context, token string) (int64, error) {
	if id, ok := f.valid[token]; ok {
		return id, nil
	}
	return 0, ErrNotFound
}

func TestRequireSession(t *testing.T) {
	a := fakeAuth{valid: map[string]int64{"ok": 42}}
	var seen int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = UserID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	t.Run("con sesión", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/player", nil)
		req.AddCookie(&http.Cookie{Name: CookieName, Value: "ok"})
		rec := httptest.NewRecorder()
		RequireSession(a, RedirectToLogin, next).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || seen != 42 {
			t.Fatalf("status %d user %d", rec.Code, seen)
		}
	})
	t.Run("sin sesión redirige", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RequireSession(a, RedirectToLogin, next).ServeHTTP(rec, httptest.NewRequest("GET", "/player", nil))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("status %d location %q", rec.Code, rec.Header().Get("Location"))
		}
	})
	t.Run("sin sesión 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stream/playlist.m3u8", nil)
		req.AddCookie(&http.Cookie{Name: CookieName, Value: "malo"})
		rec := httptest.NewRecorder()
		RequireSession(a, Unauthorized, next).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("status %d ct %q", rec.Code, rec.Header().Get("Content-Type"))
		}
	})
}

func TestSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSessionCookie(rec, "tok", time.Hour, true)
	c := rec.Result().Cookies()[0]
	if c.Name != CookieName || c.Value != "tok" || !c.HttpOnly || !c.Secure || c.Path != "/" ||
		c.SameSite != http.SameSiteLaxMode || c.MaxAge != 3600 {
		t.Fatalf("cookie inesperada: %+v", c)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)
	if TokenFromRequest(req) != "tok" {
		t.Fatal("TokenFromRequest")
	}
	rec = httptest.NewRecorder()
	ClearSessionCookie(rec, false)
	if c := rec.Result().Cookies()[0]; c.MaxAge != -1 || c.Value != "" {
		t.Fatalf("clear: %+v", c)
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/auth/ -run 'TestRequireSession|TestSessionCookie' -v`
Expected: FAIL.

- [ ] **Step 3: Implementar `middleware.go`**

```go
package auth

import (
	"context"
	"net/http"
	"time"
)

// CookieName es el nombre de la cookie de sesión.
const CookieName = "session"

// Authenticator resuelve un token de sesión a un id de usuario.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (int64, error)
}

// FailureMode define qué hacer cuando no hay sesión válida.
type FailureMode int

const (
	// RedirectToLogin responde 303 a /login (páginas).
	RedirectToLogin FailureMode = iota
	// Unauthorized responde 401 JSON (stream, SSE).
	Unauthorized
)

type ctxKey struct{}

// Escribe la cookie de sesión
//
// @param [http.ResponseWriter] w: respuesta
// @param [string] token: token de sesión
// @param [time.Duration] ttl: duración de la cookie
// @param [bool] secure: flag Secure
func SetSessionCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Borra la cookie de sesión en el navegador
//
// @param [http.ResponseWriter] w: respuesta
// @param [bool] secure: flag Secure (debe coincidir con la cookie original)
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Extrae el token de sesión del request
//
// @param [*http.Request] r: request
//
// @return [string] token o cadena vacía si no hay cookie
func TokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// Middleware que exige sesión válida e inyecta el id de usuario en el contexto
//
// @param [Authenticator] a: validador de tokens
// @param [FailureMode] mode: redirección o 401 ante fallo
// @param [http.Handler] next: handler protegido
//
// @return [http.Handler] handler envuelto
func RequireSession(a Authenticator, mode FailureMode, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := a.Authenticate(r.Context(), TokenFromRequest(r))
		if err != nil {
			if mode == Unauthorized {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"Debes iniciar sesión para ver el stream"}`))
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

// Recupera el id de usuario inyectado por RequireSession
//
// @param [context.Context] ctx: contexto del request
//
// @return [int64] id de usuario
// @return [bool] false si no hay sesión en el contexto
func UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(ctxKey{}).(int64)
	return id, ok
}
```

- [ ] **Step 4: Ejecutar tests**

Run: `go test -race ./internal/auth/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/middleware.go internal/auth/middleware_test.go
git commit -m "feat(auth): cookie de sesión y middleware RequireSession"
```

---

### Task 14: `db` — pool y migraciones embebidas

**Files:**
- Create: `migrations/embed.go`, `migrations/0001_init.sql`, `internal/db/db.go`, `internal/db/migrate.go`, `internal/db/db_test.go`, `docker-compose.dev.yml` (solo el servicio `db` por ahora; se completa en la Tarea 23)
- Modify: `go.mod` (agrega `github.com/jackc/pgx/v5`)

**Interfaces:**
- Produces:
  - `migrations.FS embed.FS`
  - `func Connect(ctx, url string, maxConns int32) (*pgxpool.Pool, error)`
  - `func Migrate(ctx, pool *pgxpool.Pool, fsys fs.FS) (int, error)` — aplica los `*.sql` pendientes en orden lexicográfico; devuelve cuántos aplicó
  - helper de test `testPool(t *testing.T) *pgxpool.Pool` (skip sin `TEST_DATABASE_URL`, migra y trunca)

- [ ] **Step 1: Dependencia y compose de desarrollo**

Run: `go get github.com/jackc/pgx/v5@latest`

`docker-compose.dev.yml`:

```yaml
services:
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: zapping
      POSTGRES_PASSWORD: zapping
      POSTGRES_DB: zapping
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U zapping -d zapping"]
      interval: 5s
      timeout: 3s
      retries: 10
```

Run: `docker compose -f docker-compose.dev.yml up -d db`
Exportar para los tests: `TEST_DATABASE_URL=postgres://zapping:zapping@localhost:5432/zapping?sslmode=disable`

- [ ] **Step 2: Escribir la migración `migrations/0001_init.sql`**

```sql
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    email         TEXT NOT NULL UNIQUE,
    password_hash BYTEA NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash BYTEA PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
```

`migrations/embed.go`:

```go
// Package migrations embebe los archivos SQL versionados del esquema.
package migrations

import "embed"

// FS contiene los archivos NNNN_nombre.sql en orden de aplicación.
//
//go:embed *.sql
var FS embed.FS
```

- [ ] **Step 3: Escribir los tests**

```go
package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"prueba-zapping/migrations"
)

// testPool conecta a TEST_DATABASE_URL, migra y deja las tablas vacías.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL no definida; se omite el test de integración")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := Connect(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE sessions, users RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestMigrate_EsIdempotente(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	n, err := Migrate(ctx, pool, migrations.FS)
	if err != nil || n != 0 {
		t.Fatalf("segunda corrida: n=%d err=%v", n, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count < 1 {
		t.Fatalf("schema_migrations: %d %v", count, err)
	}
}
```

- [ ] **Step 4: Ejecutar y ver que falla**

Run: `TEST_DATABASE_URL=postgres://zapping:zapping@localhost:5432/zapping?sslmode=disable go test ./internal/db/ -v`
Expected: FAIL (no definidos).

- [ ] **Step 5: Implementar `db.go`**

```go
// Package db conecta con PostgreSQL, aplica migraciones e implementa los
// stores de auth sobre pgx.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Abre un pool de conexiones y verifica conectividad
//
// @param [context.Context] ctx: contexto
// @param [string] url: DSN de PostgreSQL
// @param [int32] maxConns: tamaño máximo del pool
//
// @return [*pgxpool.Pool] pool listo
// @return [error] si la configuración o el ping fallan
func Connect(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("db: DATABASE_URL inválida: %w", err)
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: crear pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: no se pudo conectar: %w", err)
	}
	return pool, nil
}
```

- [ ] **Step 6: Implementar `migrate.go`**

```go
package db

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationLock es la clave del advisory lock que serializa migraciones entre réplicas.
const migrationLock = 8816_2026

// Aplica las migraciones pendientes de fsys en orden lexicográfico
//
// @param [context.Context] ctx: contexto
// @param [*pgxpool.Pool] pool: conexión
// @param [fs.FS] fsys: sistema de archivos con *.sql
//
// @return [int] migraciones aplicadas en esta corrida
// @return [error] error de lectura o SQL (la migración fallida se revierte)
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (int, error) {
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return 0, fmt.Errorf("db: listar migraciones: %w", err)
	}
	sort.Strings(files)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("db: adquirir conexión: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return 0, fmt.Errorf("db: advisory lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLock)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return 0, fmt.Errorf("db: crear schema_migrations: %w", err)
	}

	applied := 0
	for _, name := range files {
		version := strings.TrimSuffix(name, ".sql")
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists); err != nil {
			return applied, fmt.Errorf("db: consultar migración %s: %w", version, err)
		}
		if exists {
			continue
		}
		sqlBytes, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, fmt.Errorf("db: leer %s: %w", name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return applied, fmt.Errorf("db: iniciar transacción: %w", err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			tx.Rollback(ctx)
			return applied, fmt.Errorf("db: aplicar %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			tx.Rollback(ctx)
			return applied, fmt.Errorf("db: registrar %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("db: confirmar %s: %w", name, err)
		}
		applied++
	}
	return applied, nil
}
```

- [ ] **Step 7: Ejecutar tests (con y sin DB)**

Run: `TEST_DATABASE_URL=postgres://zapping:zapping@localhost:5432/zapping?sslmode=disable go test -race ./internal/db/ -v`
Expected: PASS.
Run: `go test ./internal/db/ -v`
Expected: SKIP con el mensaje de `TEST_DATABASE_URL`.

- [ ] **Step 8: Commit**

```bash
go mod tidy
git add go.mod go.sum migrations internal/db docker-compose.dev.yml
git commit -m "feat(db): pool pgx y migraciones SQL embebidas con advisory lock"
```

---

### Task 15: `db` — stores Postgres de usuarios y sesiones

**Files:**
- Create: `internal/db/users.go`, `internal/db/sessions.go`, `internal/db/stores_test.go`

**Interfaces:**
- Consumes: `auth.User`, `auth.Session`, `auth.ErrEmailTaken`, `auth.ErrNotFound`.
- Produces: `func NewUserStore(pool) *UserStore` (implementa `auth.UserStore`), `func NewSessionStore(pool) *SessionStore` (implementa `auth.SessionStore`).

- [ ] **Step 1: Escribir los tests**

```go
package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"prueba-zapping/internal/auth"
)

func TestUserStore(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserStore(pool)

	u, err := users.Create(ctx, "Ana", "ana@example.com", []byte("hash"))
	if err != nil || u.ID == 0 || u.CreatedAt.IsZero() {
		t.Fatalf("create: %v %+v", err, u)
	}
	if _, err := users.Create(ctx, "Otra", "ana@example.com", []byte("hash")); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("duplicado: %v", err)
	}
	got, err := users.FindByEmail(ctx, "ana@example.com")
	if err != nil || got.ID != u.ID || got.Name != "Ana" || string(got.PasswordHash) != "hash" {
		t.Fatalf("find: %v %+v", err, got)
	}
	if _, err := users.FindByEmail(ctx, "nadie@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
}

func TestSessionStore(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	u, _ := NewUserStore(pool).Create(ctx, "Ana", "ana@example.com", []byte("hash"))
	sessions := NewSessionStore(pool)

	live := auth.Session{TokenHash: auth.HashToken("viva"), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	dead := auth.Session{TokenHash: auth.HashToken("muerta"), UserID: u.ID, ExpiresAt: time.Now().Add(-time.Minute)}
	for _, s := range []auth.Session{live, dead} {
		if err := sessions.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := sessions.Get(ctx, live.TokenHash)
	if err != nil || got.UserID != u.ID || got.ExpiresAt.Sub(live.ExpiresAt).Abs() > time.Second {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := sessions.Get(ctx, dead.TokenHash); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expirada: %v", err)
	}
	if n, err := sessions.DeleteExpired(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("delete expired: %d %v", n, err)
	}
	if err := sessions.Delete(ctx, live.TokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Get(ctx, live.TokenHash); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("borrada: %v", err)
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `TEST_DATABASE_URL=postgres://zapping:zapping@localhost:5432/zapping?sslmode=disable go test ./internal/db/ -run 'TestUserStore|TestSessionStore' -v`
Expected: FAIL.

- [ ] **Step 3: Implementar `users.go`**

```go
package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"prueba-zapping/internal/auth"
)

// UserStore implementa auth.UserStore sobre PostgreSQL.
type UserStore struct {
	pool *pgxpool.Pool
}

// Crea el store de usuarios
//
// @param [*pgxpool.Pool] pool: conexión
//
// @return [*UserStore] store
func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

// Inserta un usuario; el email debe ser único
//
// @param [context.Context] ctx: contexto
// @param [string] name: nombre
// @param [string] email: email normalizado
// @param [[]byte] passwordHash: hash bcrypt
//
// @return [auth.User] usuario con ID y CreatedAt
// @return [error] auth.ErrEmailTaken si el email existe, u otro error SQL
func (s *UserStore) Create(ctx context.Context, name, email string, passwordHash []byte) (auth.User, error) {
	u := auth.User{Name: name, Email: email, PasswordHash: passwordHash}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (name, email, password_hash) VALUES ($1, $2, $3) RETURNING id, created_at`,
		name, email, passwordHash).Scan(&u.ID, &u.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return auth.User{}, auth.ErrEmailTaken
	}
	if err != nil {
		return auth.User{}, err
	}
	return u, nil
}

// Busca un usuario por email
//
// @param [context.Context] ctx: contexto
// @param [string] email: email normalizado
//
// @return [auth.User] usuario
// @return [error] auth.ErrNotFound si no existe
func (s *UserStore) FindByEmail(ctx context.Context, email string) (auth.User, error) {
	var u auth.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, email, password_hash, created_at FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.User{}, err
	}
	return u, nil
}
```

- [ ] **Step 4: Implementar `sessions.go`**

```go
package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prueba-zapping/internal/auth"
)

// SessionStore implementa auth.SessionStore sobre PostgreSQL.
type SessionStore struct {
	pool *pgxpool.Pool
}

// Crea el store de sesiones
//
// @param [*pgxpool.Pool] pool: conexión
//
// @return [*SessionStore] store
func NewSessionStore(pool *pgxpool.Pool) *SessionStore { return &SessionStore{pool: pool} }

// Inserta una sesión
//
// @param [context.Context] ctx: contexto
// @param [auth.Session] sess: sesión con hash, usuario y expiración
//
// @return [error] error SQL
func (s *SessionStore) Create(ctx context.Context, sess auth.Session) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		sess.TokenHash, sess.UserID, sess.ExpiresAt)
	return err
}

// Obtiene una sesión vigente por hash
//
// @param [context.Context] ctx: contexto
// @param [[]byte] tokenHash: hash del token
//
// @return [auth.Session] sesión
// @return [error] auth.ErrNotFound si no existe o expiró
func (s *SessionStore) Get(ctx context.Context, tokenHash []byte) (auth.Session, error) {
	sess := auth.Session{TokenHash: tokenHash}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, expires_at FROM sessions WHERE token_hash = $1 AND expires_at > now()`, tokenHash).
		Scan(&sess.UserID, &sess.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.Session{}, err
	}
	return sess, nil
}

// Elimina una sesión
//
// @param [context.Context] ctx: contexto
// @param [[]byte] tokenHash: hash del token
//
// @return [error] error SQL
func (s *SessionStore) Delete(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// Elimina las sesiones expiradas respecto de now
//
// @param [context.Context] ctx: contexto
// @param [time.Time] now: instante de referencia
//
// @return [int64] filas eliminadas
// @return [error] error SQL
func (s *SessionStore) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 5: Ejecutar tests y verificar las interfaces**

Agregar al final de `stores_test.go`:

```go
var (
	_ auth.UserStore    = (*UserStore)(nil)
	_ auth.SessionStore = (*SessionStore)(nil)
)
```

Run: `TEST_DATABASE_URL=postgres://zapping:zapping@localhost:5432/zapping?sslmode=disable go test -race ./internal/db/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/db/users.go internal/db/sessions.go internal/db/stores_test.go
git commit -m "feat(db): stores PostgreSQL de usuarios y sesiones"
```

---

### Task 16: `web` — vistas, estilos Neumorphism y página de registro

**Files:**
- Create: `internal/web/templates.go`, `internal/web/templates/layout.html`, `internal/web/templates/register.html`, `internal/web/static/app.css`, `internal/web/server.go`, `internal/web/server_test.go`

**Interfaces:**
- Consumes: `auth.Service` (`Register`, `Authenticate`), `auth.SetSessionCookie`, `auth.TokenFromRequest`, `auth.ValidationErrors`, `auth.ErrEmailTaken`.
- Produces:
  - `type Deps struct { Auth *auth.Service; Stream http.Handler; Ready func(ctx) error; SessionTTL time.Duration; CookieSecure bool; Logger *slog.Logger }`
  - `func New(d Deps) (*Server, error)`, `func (s *Server) Handler() http.Handler`
  - `type pageData struct { Title string; Form, Errors map[string]string; Error string }`, `func newPageData(title string) pageData`
  - `func (s *Server) isLoggedIn(r *http.Request) bool`
  - Rutas de esta tarea: `GET /register`, `POST /register`, `GET /static/{path...}`

- [ ] **Step 1: Escribir los tests**

```go
package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"prueba-zapping/internal/auth"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestServer(t *testing.T) (*Server, *auth.Service) {
	t.Helper()
	a := auth.NewService(auth.NewMemoryUserStore(), auth.NewMemorySessionStore(), time.Hour)
	s, err := New(Deps{
		Auth:       a,
		Stream:     http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "STREAM "+r.URL.Path) }),
		Ready:      func(context.Context) error { return nil },
		SessionTTL: time.Hour,
		Logger:     quietLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, a
}

func postForm(h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("sin cookie de sesión")
	return nil
}

func TestRegister_GET(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/register", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Crear cuenta") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestRegister_POST(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	ok := url.Values{"name": {"Ana"}, "email": {"ana@example.com"}, "password": {"secreto123"}}

	rec := postForm(h, "/register", ok)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/player" {
		t.Fatalf("registro ok: status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	sessionCookie(t, rec)

	rec = postForm(h, "/register", ok)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Ya existe una cuenta") {
		t.Fatalf("duplicado: status %d", rec.Code)
	}

	rec = postForm(h, "/register", url.Values{"name": {""}, "email": {"no-es-email"}, "password": {"123"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("inválido: status %d", rec.Code)
	}
	for _, msg := range []string{"El nombre es obligatorio", "El email no es válido", "al menos 8 caracteres", `value="no-es-email"`} {
		if !strings.Contains(rec.Body.String(), msg) {
			t.Errorf("falta %q en el formulario", msg)
		}
	}
}

func TestStatic(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/static/app.css", nil))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("status %d ct %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("cache-control %q", rec.Header().Get("Cache-Control"))
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/web/ -v`
Expected: FAIL.

- [ ] **Step 3: Crear `templates/layout.html`**

```html
{{define "layout"}}<!DOCTYPE html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} · Zapping Live</title>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <main class="shell">
    {{template "content" .}}
  </main>
  {{block "scripts" .}}{{end}}
</body>
</html>
{{end}}
```

- [ ] **Step 4: Crear `templates/register.html`**

```html
{{define "content"}}
<section class="card card--form">
  <h1>Crear cuenta</h1>
  <p class="muted">Regístrate para ver el livestream.</p>
  {{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
  <form method="post" action="/register" novalidate>
    <label class="field">
      <span>Nombre</span>
      <input type="text" name="name" value="{{.Form.name}}" maxlength="100" autocomplete="name" required>
      {{with .Errors.name}}<small class="field-error">{{.}}</small>{{end}}
    </label>
    <label class="field">
      <span>Email</span>
      <input type="email" name="email" value="{{.Form.email}}" autocomplete="email" required>
      {{with .Errors.email}}<small class="field-error">{{.}}</small>{{end}}
    </label>
    <label class="field">
      <span>Contraseña</span>
      <input type="password" name="password" minlength="8" maxlength="72" autocomplete="new-password" required>
      {{with .Errors.password}}<small class="field-error">{{.}}</small>{{end}}
    </label>
    <button type="submit" class="btn btn--primary">Crear cuenta</button>
  </form>
  <p class="muted">¿Ya tienes cuenta? <a href="/login">Inicia sesión</a></p>
</section>
{{end}}
```

- [ ] **Step 5: Crear `static/app.css` (Neumorphism)**

```css
:root {
  --bg: #e0e5ec;
  --text: #2f3640;
  --muted: #6c7a89;
  --accent: #3d5afe;
  --danger: #d63031;
  --live: #e84118;
  --light: rgba(255, 255, 255, 0.9);
  --dark: rgba(163, 177, 198, 0.7);
  --radius: 20px;
}

* { box-sizing: border-box; }

body {
  margin: 0;
  min-height: 100vh;
  background: var(--bg);
  color: var(--text);
  font-family: "Segoe UI", system-ui, -apple-system, sans-serif;
  display: flex;
  align-items: center;
  justify-content: center;
}

.shell { width: min(100% - 2rem, 1100px); padding: 2rem 0; }

.card {
  background: var(--bg);
  border-radius: var(--radius);
  padding: 2rem;
  box-shadow: 10px 10px 24px var(--dark), -10px -10px 24px var(--light);
}

.card--form { max-width: 420px; margin: 0 auto; }

h1 { margin: 0 0 0.25rem; font-size: 1.6rem; }
.muted { color: var(--muted); margin: 0.5rem 0 1rem; }

.field { display: block; margin-bottom: 1rem; }
.field span { display: block; font-size: 0.85rem; margin-bottom: 0.35rem; color: var(--muted); }

input {
  width: 100%;
  border: none;
  outline: none;
  padding: 0.8rem 1rem;
  border-radius: 14px;
  background: var(--bg);
  color: var(--text);
  font-size: 1rem;
  box-shadow: inset 6px 6px 12px var(--dark), inset -6px -6px 12px var(--light);
}
input:focus { box-shadow: inset 6px 6px 12px var(--dark), inset -6px -6px 12px var(--light), 0 0 0 2px var(--accent); }

.btn {
  display: inline-block;
  width: 100%;
  border: none;
  cursor: pointer;
  padding: 0.85rem 1.2rem;
  border-radius: 14px;
  background: var(--bg);
  color: var(--text);
  font-size: 1rem;
  font-weight: 600;
  box-shadow: 6px 6px 14px var(--dark), -6px -6px 14px var(--light);
  transition: box-shadow 0.15s ease, transform 0.15s ease;
}
.btn:hover { transform: translateY(-1px); }
.btn:active { box-shadow: inset 6px 6px 12px var(--dark), inset -6px -6px 12px var(--light); transform: none; }
.btn--primary { color: var(--accent); }
.btn--ghost { width: auto; padding: 0.5rem 1rem; font-size: 0.9rem; }

.alert {
  padding: 0.75rem 1rem;
  border-radius: 14px;
  color: var(--danger);
  box-shadow: inset 4px 4px 8px var(--dark), inset -4px -4px 8px var(--light);
}
.field-error { display: block; margin-top: 0.35rem; color: var(--danger); font-size: 0.8rem; }

a { color: var(--accent); text-decoration: none; }
a:hover { text-decoration: underline; }
```

- [ ] **Step 6: Implementar `templates.go`**

```go
// Package web sirve las páginas del sitio (registro, login, player), los
// assets estáticos y el canal de eventos SSE. Usa los paquetes auth y stream
// solo a través de sus interfaces públicas.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// pageData es el modelo común de todas las vistas.
type pageData struct {
	Title  string
	Form   map[string]string
	Errors map[string]string
	Error  string
}

// Crea el modelo de una vista con los mapas inicializados
//
// @param [string] title: título de la página
//
// @return [pageData] modelo listo para renderizar
func newPageData(title string) pageData {
	return pageData{Title: title, Form: map[string]string{}, Errors: map[string]string{}}
}

// renderer mantiene una plantilla compilada por página, cada una con el layout.
type renderer struct {
	pages map[string]*template.Template
}

// Compila layout + cada página embebida
//
// @return [*renderer] renderer listo
// @return [error] si alguna plantilla no compila
func newRenderer() (*renderer, error) {
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	r := &renderer{pages: make(map[string]*template.Template)}
	for _, full := range names {
		name := path.Base(full)
		if name == "layout.html" {
			continue
		}
		t, err := template.ParseFS(templateFS, "templates/layout.html", full)
		if err != nil {
			return nil, fmt.Errorf("web: compilar %s: %w", name, err)
		}
		r.pages[name] = t
	}
	return r, nil
}

// Renderiza una página completa en un buffer y la escribe con el status dado
//
// @param [http.ResponseWriter] w: respuesta
// @param [int] status: código HTTP
// @param [string] page: nombre de archivo de la página (ej. "login.html")
// @param [any] data: modelo de la vista
//
// @return [error] si la página no existe o falla la ejecución (no se escribió nada)
func (r *renderer) render(w http.ResponseWriter, status int, page string, data any) error {
	t, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("web: página %q no registrada", page)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		return fmt.Errorf("web: renderizar %s: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}
```

- [ ] **Step 7: Implementar `server.go`**

```go
package web

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"prueba-zapping/internal/auth"
)

// Deps son las dependencias del sitio, inyectadas desde cmd/server.
type Deps struct {
	Auth         *auth.Service
	Stream       http.Handler // handler del stream sin autenticación; se protege aquí
	Ready        func(ctx context.Context) error
	SessionTTL   time.Duration
	CookieSecure bool
	Logger       *slog.Logger
}

// Server agrupa rutas y handlers de las páginas.
type Server struct {
	deps Deps
	r    *renderer
	mux  *http.ServeMux
}

// Construye el servidor web y registra sus rutas
//
// @param [Deps] d: dependencias
//
// @return [*Server] servidor
// @return [error] si las plantillas no compilan
func New(d Deps) (*Server, error) {
	r, err := newRenderer()
	if err != nil {
		return nil, err
	}
	s := &Server{deps: d, r: r, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// Handler HTTP con todas las rutas del sitio
//
// @return [http.Handler] mux
func (s *Server) Handler() http.Handler { return s.mux }

// Registra las rutas en el mux
func (s *Server) routes() {
	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", cacheControl("public, max-age=86400", http.StripPrefix("/static/", http.FileServerFS(static))))
	s.mux.HandleFunc("GET /register", s.registerForm)
	s.mux.HandleFunc("POST /register", s.registerSubmit)
}

// Envuelve un handler fijando el header Cache-Control
//
// @param [string] value: valor del header
// @param [http.Handler] next: handler envuelto
//
// @return [http.Handler] handler con el header
func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}

// Indica si el request trae una sesión válida
//
// @param [*http.Request] r: request
//
// @return [bool] true si la cookie corresponde a una sesión vigente
func (s *Server) isLoggedIn(r *http.Request) bool {
	_, err := s.deps.Auth.Authenticate(r.Context(), auth.TokenFromRequest(r))
	return err == nil
}

// Renderiza una vista y registra el error si falla
//
// @param [http.ResponseWriter] w: respuesta
// @param [int] status: código HTTP
// @param [string] page: página
// @param [pageData] data: modelo
func (s *Server) render(w http.ResponseWriter, status int, page string, data pageData) {
	if err := s.r.render(w, status, page, data); err != nil {
		s.deps.Logger.Error("no se pudo renderizar la vista", "page", page, "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
	}
}

// Muestra el formulario de registro (o redirige al player si ya hay sesión)
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) registerForm(w http.ResponseWriter, r *http.Request) {
	if s.isLoggedIn(r) {
		http.Redirect(w, r, "/player", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "register.html", newPageData("Crear cuenta"))
}

// Procesa el registro: crea la cuenta, abre sesión y redirige al player
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request con el formulario
func (s *Server) registerSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formulario inválido", http.StatusBadRequest)
		return
	}
	in := auth.RegistrationInput{
		Name:     r.PostFormValue("name"),
		Email:    r.PostFormValue("email"),
		Password: r.PostFormValue("password"),
	}
	_, token, err := s.deps.Auth.Register(r.Context(), in)
	if err != nil {
		data := newPageData("Crear cuenta")
		data.Form["name"] = in.Name
		data.Form["email"] = in.Email
		var verr auth.ValidationErrors
		switch {
		case errors.As(err, &verr):
			data.Errors = verr.ByField()
			s.render(w, http.StatusUnprocessableEntity, "register.html", data)
		case errors.Is(err, auth.ErrEmailTaken):
			data.Errors["email"] = "Ya existe una cuenta con ese email"
			s.render(w, http.StatusConflict, "register.html", data)
		default:
			s.deps.Logger.Error("registro falló", "error", err)
			data.Error = "No pudimos crear tu cuenta. Inténtalo de nuevo."
			s.render(w, http.StatusInternalServerError, "register.html", data)
		}
		return
	}
	auth.SetSessionCookie(w, token, s.deps.SessionTTL, s.deps.CookieSecure)
	http.Redirect(w, r, "/player", http.StatusSeeOther)
}
```

- [ ] **Step 8: Ejecutar tests**

Run: `go test -race ./internal/web/ -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/web
git commit -m "feat(web): vistas con estilo Neumorphism y página de crear cuenta"
```

---

### Task 17: `web` — login, logout y redirección raíz

**Files:**
- Create: `internal/web/templates/login.html`
- Modify: `internal/web/server.go` (rutas y handlers), `internal/web/server_test.go`

**Interfaces:**
- Consumes: `auth.Service.Login`, `Logout`, `auth.ClearSessionCookie`, `auth.ErrInvalidCredentials`.
- Produces rutas: `GET /{$}`, `GET /login`, `POST /login`, `POST /logout`.

- [ ] **Step 1: Agregar tests**

```go
func registerAndLogin(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := postForm(h, "/register", url.Values{"name": {"Ana"}, "email": {"ana@example.com"}, "password": {"secreto123"}})
	return sessionCookie(t, rec)
}

func TestLogin(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	registerAndLogin(t, h)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Iniciar sesión") {
		t.Fatalf("GET: %d", rec.Code)
	}

	rec = postForm(h, "/login", url.Values{"email": {"ana@example.com"}, "password": {"incorrecta"}})
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Email o contraseña incorrectos") {
		t.Fatalf("credenciales inválidas: %d", rec.Code)
	}

	rec = postForm(h, "/login", url.Values{"email": {"ana@example.com"}, "password": {"secreto123"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/player" {
		t.Fatalf("login ok: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	c := sessionCookie(t, rec)

	req := httptest.NewRequest("GET", "/login", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/player" {
		t.Fatalf("login con sesión debía redirigir: %d", rec.Code)
	}
}

func TestLogoutYRaiz(t *testing.T) {
	s, a := newTestServer(t)
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("raíz sin sesión: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	c := registerAndLogin(t, h)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Location") != "/player" {
		t.Fatalf("raíz con sesión: %q", rec.Header().Get("Location"))
	}

	req = httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("logout: %d", rec.Code)
	}
	if cleared := rec.Result().Cookies()[0]; cleared.MaxAge != -1 {
		t.Fatalf("la cookie debía borrarse: %+v", cleared)
	}
	if _, err := a.Authenticate(context.Background(), c.Value); err == nil {
		t.Fatal("la sesión debía quedar invalidada")
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/web/ -run 'TestLogin|TestLogout' -v`
Expected: FAIL (404 en las rutas nuevas).

- [ ] **Step 3: Crear `templates/login.html`**

```html
{{define "content"}}
<section class="card card--form">
  <h1>Iniciar sesión</h1>
  <p class="muted">Entra para ver el livestream.</p>
  {{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
  <form method="post" action="/login" novalidate>
    <label class="field">
      <span>Email</span>
      <input type="email" name="email" value="{{.Form.email}}" autocomplete="email" required>
    </label>
    <label class="field">
      <span>Contraseña</span>
      <input type="password" name="password" autocomplete="current-password" required>
    </label>
    <button type="submit" class="btn btn--primary">Entrar</button>
  </form>
  <p class="muted">¿No tienes cuenta? <a href="/register">Crea una</a></p>
</section>
{{end}}
```

- [ ] **Step 4: Agregar rutas y handlers en `server.go`**

En `routes()`:

```go
	s.mux.HandleFunc("GET /{$}", s.root)
	s.mux.HandleFunc("GET /login", s.loginForm)
	s.mux.HandleFunc("POST /login", s.loginSubmit)
	s.mux.HandleFunc("POST /logout", s.logout)
```

Handlers:

```go
// Redirige la raíz al player o al login según haya sesión
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	if s.isLoggedIn(r) {
		http.Redirect(w, r, "/player", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Muestra el formulario de login (o redirige al player si ya hay sesión)
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if s.isLoggedIn(r) {
		http.Redirect(w, r, "/player", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "login.html", newPageData("Iniciar sesión"))
}

// Procesa el login: valida credenciales, abre sesión y redirige al player
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request con el formulario
func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formulario inválido", http.StatusBadRequest)
		return
	}
	email, password := r.PostFormValue("email"), r.PostFormValue("password")
	_, token, err := s.deps.Auth.Login(r.Context(), email, password)
	if err != nil {
		data := newPageData("Iniciar sesión")
		data.Form["email"] = email
		if errors.Is(err, auth.ErrInvalidCredentials) {
			data.Error = "Email o contraseña incorrectos"
			s.render(w, http.StatusUnauthorized, "login.html", data)
			return
		}
		s.deps.Logger.Error("login falló", "error", err)
		data.Error = "No pudimos iniciar sesión. Inténtalo de nuevo."
		s.render(w, http.StatusInternalServerError, "login.html", data)
		return
	}
	auth.SetSessionCookie(w, token, s.deps.SessionTTL, s.deps.CookieSecure)
	http.Redirect(w, r, "/player", http.StatusSeeOther)
}

// Cierra la sesión actual, borra la cookie y redirige al login
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if token := auth.TokenFromRequest(r); token != "" {
		if err := s.deps.Auth.Logout(r.Context(), token); err != nil {
			s.deps.Logger.Error("logout falló", "error", err)
		}
	}
	auth.ClearSessionCookie(w, s.deps.CookieSecure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
```

- [ ] **Step 5: Ejecutar tests**

Run: `go test -race ./internal/web/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web
git commit -m "feat(web): páginas de login y logout con redirección de la raíz"
```

---

### Task 18: `web` — página del player y montaje protegido del stream

**Files:**
- Create: `internal/web/templates/player.html`, `internal/web/static/player.js`
- Modify: `internal/web/server.go`, `internal/web/static/app.css`, `internal/web/server_test.go`

**Interfaces:**
- Consumes: `auth.RequireSession`, `auth.RedirectToLogin`, `auth.Unauthorized`, `Deps.Stream`.
- Produces rutas: `GET /player` (sesión, redirect), `/stream/` (sesión, 401) → `http.StripPrefix("/stream", Deps.Stream)`.

- [ ] **Step 1: Agregar tests**

```go
func TestPlayerYStreamProtegidos(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/player", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("player sin sesión: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/stream/playlist.m3u8", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stream sin sesión: %d", rec.Code)
	}

	c := registerAndLogin(t, h)
	req := httptest.NewRequest("GET", "/player", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "hls.js") || !strings.Contains(rec.Body.String(), `id="video"`) {
		t.Fatalf("player con sesión: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/stream/playlist.m3u8", nil)
	req.AddCookie(c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "STREAM /playlist.m3u8" {
		t.Fatalf("stream con sesión: %d %q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/web/ -run TestPlayerYStream -v`
Expected: FAIL.

- [ ] **Step 3: Crear `templates/player.html`**

```html
{{define "content"}}
<header class="topbar">
  <div class="brand"><span class="live-dot" id="live-dot"></span> Zapping Live <span class="status" id="status">Conectando…</span></div>
  <form method="post" action="/logout"><button type="submit" class="btn btn--ghost">Cerrar sesión</button></form>
</header>
<section class="player-grid">
  <div class="card card--video">
    <video id="video" controls autoplay muted playsinline></video>
  </div>
  <aside class="card panel">
    <h2>En vivo</h2>
    <dl class="stats">
      <div><dt>Espectadores</dt><dd id="viewers">—</dd></div>
      <div><dt>Secuencia</dt><dd id="sequence">—</dd></div>
      <div><dt>Discontinuidades</dt><dd id="disc-sequence">—</dd></div>
      <div><dt>Próximo segmento en</dt><dd id="countdown">—</dd></div>
    </dl>
    <h3>Ventana actual</h3>
    <ol class="segments" id="segments"><li class="muted">Esperando datos…</li></ol>
  </aside>
</section>
{{end}}
{{define "scripts"}}
<script src="https://cdn.jsdelivr.net/npm/hls.js@1/dist/hls.min.js"></script>
<script src="/static/player.js"></script>
{{end}}
```

- [ ] **Step 4: Crear `static/player.js` (solo reproducción; el panel se completa en la Tarea 20)**

```js
(function () {
  'use strict';

  var SOURCE = '/stream/playlist.m3u8';
  var video = document.getElementById('video');
  var hls = null;

  // Arranca (o reinicia) la reproducción HLS con HLS.js o con soporte nativo
  function startPlayer() {
    if (window.Hls && Hls.isSupported()) {
      if (hls) { hls.destroy(); }
      hls = new Hls({ liveSyncDurationCount: 2 });
      hls.loadSource(SOURCE);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, function () { video.play().catch(function () {}); });
      hls.on(Hls.Events.ERROR, function (_, data) {
        if (!data.fatal) { return; }
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) { hls.startLoad(); }
        else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) { hls.recoverMediaError(); }
        else { startPlayer(); }
      });
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = SOURCE;
      video.play().catch(function () {});
    }
  }

  window.ZappingPlayer = { start: startPlayer };
  startPlayer();
})();
```

- [ ] **Step 5: Agregar estilos del player al final de `app.css`**

```css
.topbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1.5rem; }
.brand { display: flex; align-items: center; gap: 0.6rem; font-weight: 700; font-size: 1.2rem; }
.status { font-weight: 400; font-size: 0.85rem; color: var(--muted); }
.live-dot { width: 12px; height: 12px; border-radius: 50%; background: var(--muted); box-shadow: inset 2px 2px 4px var(--dark), inset -2px -2px 4px var(--light); }
.live-dot.on { background: var(--live); box-shadow: 0 0 10px var(--live); animation: pulse 1.5s infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

.player-grid { display: grid; grid-template-columns: minmax(0, 2.2fr) minmax(260px, 1fr); gap: 1.5rem; }
@media (max-width: 800px) { .player-grid { grid-template-columns: 1fr; } }

.card--video { padding: 1rem; }
video { width: 100%; aspect-ratio: 16 / 9; border-radius: 14px; background: #000; box-shadow: inset 6px 6px 12px var(--dark), inset -6px -6px 12px var(--light); }

.panel h2 { margin: 0 0 1rem; }
.panel h3 { margin: 1.5rem 0 0.75rem; font-size: 1rem; color: var(--muted); }
.stats { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; margin: 0; }
.stats div { padding: 0.75rem; border-radius: 14px; box-shadow: inset 4px 4px 8px var(--dark), inset -4px -4px 8px var(--light); }
.stats dt { font-size: 0.75rem; color: var(--muted); }
.stats dd { margin: 0.25rem 0 0; font-size: 1.3rem; font-weight: 700; font-variant-numeric: tabular-nums; }

.segments { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.5rem; }
.segments li { padding: 0.6rem 0.9rem; border-radius: 12px; box-shadow: 4px 4px 8px var(--dark), -4px -4px 8px var(--light); font-family: ui-monospace, Menlo, monospace; font-size: 0.85rem; display: flex; justify-content: space-between; }
.segments li.leaving { opacity: 0.6; }
.segments li.discontinuity::before { content: "⟲ "; color: var(--live); }
```

- [ ] **Step 6: Agregar rutas en `routes()` y el handler del player**

```go
	s.mux.Handle("GET /player", auth.RequireSession(s.deps.Auth, auth.RedirectToLogin, http.HandlerFunc(s.player)))
	s.mux.Handle("/stream/", http.StripPrefix("/stream", auth.RequireSession(s.deps.Auth, auth.Unauthorized, s.deps.Stream)))
```

```go
// Muestra la página del player
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request autenticado
func (s *Server) player(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "player.html", newPageData("Player"))
}
```

- [ ] **Step 7: Ejecutar tests**

Run: `go test -race ./internal/web/ -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/web
git commit -m "feat(web): página del player con HLS.js y stream protegido por sesión"
```

---

### Task 19: `web` — hub SSE de espectadores y ventana

**Files:**
- Create: `internal/web/hub.go`, `internal/web/hub_test.go`
- Modify: `internal/web/server.go` (campo `Hub` en `Deps`, ruta `/events`)

**Interfaces:**
- Consumes: `stream.Window`, `stream.Entry`.
- Produces:
  - `func NewHub(logger *slog.Logger) *Hub`
  - `func (h *Hub) Run(ctx context.Context, events <-chan stream.Window)`
  - `func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request)`
  - `func (h *Hub) Viewers() int`
  - Eventos SSE `window` y `viewers` con el JSON de la spec §7.3 (más `secondsToNextTick`)
  - `Deps.Hub *Hub`; ruta `GET /events` (sesión, 401)

- [ ] **Step 1: Escribir los tests**

```go
package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"prueba-zapping/internal/stream"
)

// readEvent lee líneas hasta encontrar "event: <name>" y devuelve su línea data.
func readEvent(t *testing.T, sc *bufio.Scanner, name string) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("no llegó el evento %q", name)
		default:
		}
		if !sc.Scan() {
			t.Fatalf("stream cerrado esperando %q: %v", name, sc.Err())
		}
		if sc.Text() == "event: "+name {
			sc.Scan()
			return strings.TrimPrefix(sc.Text(), "data: ")
		}
	}
}

func TestHub(t *testing.T) {
	hub := NewHub(quietLogger())
	events := make(chan stream.Window, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx, events)

	srv := httptest.NewServer(hub)
	defer srv.Close()

	reqCtx, cancelReq := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(reqCtx, "GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)

	if data := readEvent(t, sc, "viewers"); data != `{"viewers":1}` {
		t.Fatalf("viewers: %s", data)
	}
	events <- stream.Window{
		MediaSequence: 5, DiscontinuitySequence: 1,
		Entries:  []stream.Entry{{Name: "a.ts", Duration: 10 * time.Second}, {Name: "b.ts", Duration: 10 * time.Second, Discontinuity: true}, {Name: "c.ts", Duration: 4 * time.Second}},
		NextTick: time.Now().Add(7 * time.Second),
	}
	data := readEvent(t, sc, "window")
	for _, want := range []string{`"sequence":5`, `"discontinuitySequence":1`, `"name":"b.ts"`, `"discontinuity":true`, `"viewers":1`, `"secondsToNextTick":`} {
		if !strings.Contains(data, want) {
			t.Errorf("falta %s en %s", want, data)
		}
	}
	if hub.Viewers() != 1 {
		t.Fatalf("viewers: %d", hub.Viewers())
	}
	cancelReq()
	for i := 0; i < 50 && hub.Viewers() != 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if hub.Viewers() != 0 {
		t.Fatal("el cliente debía darse de baja al desconectar")
	}
}

func TestHub_ClienteNuevoRecibeUltimaVentana(t *testing.T) {
	hub := NewHub(quietLogger())
	events := make(chan stream.Window, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx, events)
	events <- stream.Window{MediaSequence: 9, Entries: []stream.Entry{{Name: "x.ts"}, {Name: "y.ts"}, {Name: "z.ts"}}, NextTick: time.Now()}
	for i := 0; i < 50 && hub.lastWindow() == nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}

	srv := httptest.NewServer(hub)
	defer srv.Close()
	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	req, _ := http.NewRequestWithContext(reqCtx, "GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if data := readEvent(t, bufio.NewScanner(resp.Body), "window"); !strings.Contains(data, `"sequence":9`) {
		t.Fatalf("ventana inicial: %s", data)
	}
}
```

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/web/ -run TestHub -v`
Expected: FAIL.

- [ ] **Step 3: Implementar `hub.go`**

```go
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
```

- [ ] **Step 4: Conectar el hub en `server.go`**

Agregar a `Deps`:

```go
	Hub          *Hub
```

En `routes()`:

```go
	if s.deps.Hub != nil {
		s.mux.Handle("GET /events", auth.RequireSession(s.deps.Auth, auth.Unauthorized, s.deps.Hub))
	}
```

- [ ] **Step 5: Ejecutar tests**

Run: `go test -race ./internal/web/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/hub.go internal/web/hub_test.go internal/web/server.go
git commit -m "feat(web): hub SSE con contador de espectadores y eventos de ventana"
```

---

### Task 20: Panel en vivo del player (JS)

**Files:**
- Modify: `internal/web/static/player.js`

**Interfaces:**
- Consumes: eventos SSE `window` y `viewers` de la Tarea 19; `window.ZappingPlayer.start`.

- [ ] **Step 1: Reemplazar `player.js` por la versión completa**

```js
(function () {
  'use strict';

  var SOURCE = '/stream/playlist.m3u8';
  var video = document.getElementById('video');
  var els = {
    status: document.getElementById('status'),
    liveDot: document.getElementById('live-dot'),
    viewers: document.getElementById('viewers'),
    sequence: document.getElementById('sequence'),
    discSequence: document.getElementById('disc-sequence'),
    countdown: document.getElementById('countdown'),
    segments: document.getElementById('segments')
  };
  var hls = null;
  var lastSequence = -1;
  var nextTickLocal = null; // instante local (ms) del próximo tick

  // Arranca (o reinicia) la reproducción HLS con HLS.js o con soporte nativo
  function startPlayer() {
    if (window.Hls && Hls.isSupported()) {
      if (hls) { hls.destroy(); }
      hls = new Hls({ liveSyncDurationCount: 2 });
      hls.loadSource(SOURCE);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, function () { video.play().catch(function () {}); });
      hls.on(Hls.Events.ERROR, function (_, data) {
        if (!data.fatal) { return; }
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) { hls.startLoad(); }
        else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) { hls.recoverMediaError(); }
        else { startPlayer(); }
      });
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = SOURCE;
      video.play().catch(function () {});
    }
  }

  // Actualiza el estado de conexión en la barra superior
  function setStatus(text, live) {
    els.status.textContent = text;
    els.liveDot.classList.toggle('on', !!live);
  }

  // Pinta la ventana recibida por SSE
  function renderWindow(ev) {
    els.sequence.textContent = ev.sequence;
    els.discSequence.textContent = ev.discontinuitySequence;
    els.viewers.textContent = ev.viewers;
    nextTickLocal = Date.now() + ev.secondsToNextTick * 1000;
    els.segments.innerHTML = '';
    ev.segments.forEach(function (seg, i) {
      var li = document.createElement('li');
      if (i === 0) { li.classList.add('leaving'); }
      if (seg.discontinuity) { li.classList.add('discontinuity'); }
      var name = document.createElement('span');
      name.textContent = seg.name;
      var dur = document.createElement('span');
      dur.textContent = seg.duration.toFixed(3) + ' s';
      li.appendChild(name);
      li.appendChild(dur);
      els.segments.appendChild(li);
    });
  }

  // Cuenta regresiva local al próximo tick (4 veces por segundo)
  setInterval(function () {
    if (nextTickLocal === null) { return; }
    var remaining = Math.max(0, (nextTickLocal - Date.now()) / 1000);
    els.countdown.textContent = remaining.toFixed(1) + ' s';
  }, 250);

  // Conecta al canal SSE; EventSource reintenta solo al perder la conexión
  function connectEvents() {
    var es = new EventSource('/events');
    es.addEventListener('window', function (e) {
      var ev = JSON.parse(e.data);
      if (lastSequence >= 0 && ev.sequence < lastSequence) {
        // El servidor se reinició: la secuencia retrocedió, recargamos la fuente.
        startPlayer();
      }
      lastSequence = ev.sequence;
      renderWindow(ev);
    });
    es.addEventListener('viewers', function (e) {
      els.viewers.textContent = JSON.parse(e.data).viewers;
    });
    es.onopen = function () { setStatus('EN VIVO', true); };
    es.onerror = function () { setStatus('Reconectando…', false); };
  }

  window.ZappingPlayer = { start: startPlayer };
  startPlayer();
  connectEvents();
})();
```

- [ ] **Step 2: Verificar que el sitio compila y los tests siguen en verde**

Run: `go test -race ./internal/web/ && go vet ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/web/static/player.js
git commit -m "feat(web): panel en vivo con espectadores, secuencia, ventana y cuenta regresiva"
```

---

### Task 21: Middlewares, `/healthz` y composición en `cmd/server`

**Files:**
- Create: `internal/web/middleware.go`, `internal/web/middleware_test.go`
- Modify: `internal/web/server.go` (ruta `/healthz`), `internal/web/server_test.go`, `cmd/server/main.go`

**Interfaces:**
- Produces:
  - `func Recover(logger *slog.Logger) func(http.Handler) http.Handler`
  - `func Logging(logger *slog.Logger) func(http.Handler) http.Handler` (el writer envuelto implementa `http.Flusher` y `Unwrap()`)
  - Ruta `GET /healthz` → `Deps.Ready`
  - `cmd/server` funcional: `PORT`, `DATABASE_URL`, `SEGMENTS_DIR`…

- [ ] **Step 1: Escribir tests de middlewares y healthz**

`middleware_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecover(t *testing.T) {
	h := Recover(quietLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLogging_PreservaFlusher(t *testing.T) {
	var isFlusher bool
	h := Logging(quietLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, isFlusher = w.(http.Flusher)
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !isFlusher || rec.Code != http.StatusTeapot {
		t.Fatalf("flusher=%v status=%d", isFlusher, rec.Code)
	}
}
```

Agregar a `server_test.go`:

```go
func TestHealthz(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	s.deps.Ready = func(context.Context) error { return errors.New("stream no listo") }
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}
```

(agregar `"errors"` a los imports del test).

- [ ] **Step 2: Ejecutar y ver que falla**

Run: `go test ./internal/web/ -run 'TestRecover|TestLogging|TestHealthz' -v`
Expected: FAIL.

- [ ] **Step 3: Implementar `middleware.go`**

```go
package web

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// statusWriter captura el status y los bytes escritos; preserva Flush para SSE.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

// Registra el status antes de delegar
//
// @param [int] code: código HTTP
func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

// Escribe el cuerpo contabilizando bytes
//
// @param [[]byte] b: datos
//
// @return [int] bytes escritos
// @return [error] error de escritura
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Delegación de Flush para respuestas en streaming (SSE)
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Expone el writer original para http.ResponseController
//
// @return [http.ResponseWriter] writer envuelto
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Middleware que convierte un panic en 500 y lo registra
//
// @param [*slog.Logger] logger: logger
//
// @return [func(http.Handler) http.Handler] middleware
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic en handler", "error", rec, "path", r.URL.Path, "stack", string(debug.Stack()))
					http.Error(w, "Error interno", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Middleware que registra método, ruta, status, bytes y duración de cada request
//
// @param [*slog.Logger] logger: logger
//
// @return [func(http.Handler) http.Handler] middleware
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			logger.Info("request",
				"method", r.Method, "path", r.URL.Path, "status", sw.status,
				"bytes", sw.bytes, "duration", time.Since(start), "remote", r.RemoteAddr)
		})
	}
}
```

- [ ] **Step 4: Agregar `/healthz` en `server.go`**

En `routes()`:

```go
	s.mux.HandleFunc("GET /healthz", s.healthz)
```

```go
// Responde 200 si el stream y la base de datos están listos; 503 si no
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Ready(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}
```

- [ ] **Step 5: Reemplazar `cmd/server/main.go`**

```go
// Punto de entrada del servidor: compone configuración, base de datos, worker
// de streaming, hub SSE y servidor HTTP, y gestiona el apagado ordenado.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"prueba-zapping/internal/auth"
	"prueba-zapping/internal/config"
	"prueba-zapping/internal/db"
	"prueba-zapping/internal/stream"
	"prueba-zapping/internal/web"
	"prueba-zapping/migrations"
)

const shutdownGrace = 10 * time.Second

// Ejecuta run y termina el proceso con código 1 si falla
func main() {
	if err := run(); err != nil {
		slog.Error("el servidor terminó con error", "error", err)
		os.Exit(1)
	}
}

// Compone y ejecuta el servicio hasta recibir SIGINT/SIGTERM
//
// @return [error] error fatal de arranque o de ejecución
func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Base de datos y migraciones.
	dbCtx, cancelDB := context.WithTimeout(ctx, 30*time.Second)
	pool, err := db.Connect(dbCtx, cfg.DatabaseURL, cfg.DBMaxConns)
	cancelDB()
	if err != nil {
		return err
	}
	defer pool.Close()
	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		return err
	}
	logger.Info("migraciones aplicadas", "count", applied)

	// Stream: manifiesto, archivos y worker.
	f, err := os.Open(filepath.Join(cfg.SegmentsDir, cfg.SegmentsManifest))
	if err != nil {
		return fmt.Errorf("abrir manifiesto: %w", err)
	}
	segments, err := stream.ParseManifest(f)
	f.Close()
	if err != nil {
		return err
	}
	if err := stream.VerifyFiles(cfg.SegmentsDir, segments); err != nil {
		return err
	}
	timeline, err := stream.NewTimeline(segments)
	if err != nil {
		return err
	}
	streamSvc := stream.NewService(timeline, stream.DirLoader(cfg.SegmentsDir), stream.RealClock(), logger)

	// Auth y web.
	authSvc := auth.NewService(db.NewUserStore(pool), db.NewSessionStore(pool), cfg.SessionTTL)
	hub := web.NewHub(logger)
	site, err := web.New(web.Deps{
		Auth:   authSvc,
		Stream: stream.NewHandler(streamSvc),
		Hub:    hub,
		Ready: func(ctx context.Context) error {
			if streamSvc.Snapshot() == nil {
				return errors.New("el stream todavía no publicó su primera ventana")
			}
			return pool.Ping(ctx)
		},
		SessionTTL:   cfg.SessionTTL,
		CookieSecure: cfg.CookieSecure,
		Logger:       logger,
	})
	if err != nil {
		return err
	}

	var handler http.Handler = site.Handler()
	handler = http.NewCrossOriginProtection().Handler(handler)
	handler = web.Logging(logger)(handler)
	handler = web.Recover(logger)(handler)

	srv := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout 0: las conexiones SSE son de larga duración.
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	// Goroutines de fondo.
	errCh := make(chan error, 2)
	go func() { errCh <- streamSvc.Run(ctx) }()
	events, unsubscribe := streamSvc.Subscribe()
	defer unsubscribe()
	go hub.Run(ctx, events)
	go sessionJanitor(ctx, authSvc, logger)
	go func() {
		logger.Info("servidor HTTP escuchando", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("señal recibida; apagando")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("apagado forzado del servidor HTTP", "error", err)
	}
	return nil
}

// Limpia sesiones expiradas de la DB cada hora y de la caché cada minuto
//
// @param [context.Context] ctx: cancelación
// @param [*auth.Service] svc: servicio de auth
// @param [*slog.Logger] logger: logger
func sessionJanitor(ctx context.Context, svc *auth.Service, logger *slog.Logger) {
	dbTicker := time.NewTicker(time.Hour)
	cacheTicker := time.NewTicker(time.Minute)
	defer dbTicker.Stop()
	defer cacheTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cacheTicker.C:
			svc.SweepCache()
		case <-dbTicker.C:
			if n, err := svc.DeleteExpired(ctx); err != nil {
				logger.Error("no se pudieron borrar sesiones expiradas", "error", err)
			} else if n > 0 {
				logger.Info("sesiones expiradas eliminadas", "count", n)
			}
		}
	}
}
```

- [ ] **Step 6: Tests y arranque manual**

Run: `go test -race ./... && go vet ./...`
Expected: PASS.

Run (con el Postgres de `docker-compose.dev.yml` levantado):

```bash
DATABASE_URL='postgres://zapping:zapping@localhost:5432/zapping?sslmode=disable' SEGMENTS_DIR=./segments LOG_LEVEL=debug go run ./cmd/server
```

Expected: logs JSON `migraciones aplicadas`, `stream iniciado`, `servidor HTTP escuchando`; `curl -i localhost:8080/healthz` → `200 ok`; abrir `http://localhost:8080`, registrarse y ver el video reproducir con el panel actualizándose cada 10s. `Ctrl+C` apaga limpio.

- [ ] **Step 7: Commit**

```bash
git add cmd/server/main.go internal/web/middleware.go internal/web/middleware_test.go internal/web/server.go internal/web/server_test.go
git commit -m "feat(server): composición del servicio, middlewares, healthz y apagado ordenado"
```

---

### Task 22: Prueba end-to-end del flujo completo

**Files:**
- Create: `internal/web/e2e_test.go`

**Interfaces:**
- Consumes: todo lo anterior con stores en memoria y un `stream.Service` real sobre un manifiesto en memoria.

- [ ] **Step 1: Escribir el test**

```go
package web

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"prueba-zapping/internal/auth"
	"prueba-zapping/internal/stream"
)

func TestE2E_FlujoCompleto(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tl, err := stream.NewTimeline([]stream.Segment{
		{Name: "s0.ts", Duration: 10 * time.Second}, {Name: "s1.ts", Duration: 10 * time.Second},
		{Name: "s2.ts", Duration: 10 * time.Second}, {Name: "s3.ts", Duration: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	loader := func(name string) ([]byte, error) { return []byte("video " + name), nil }
	streamSvc := stream.NewService(tl, loader, stream.RealClock(), quietLogger())
	go streamSvc.Run(ctx)
	events, unsub := streamSvc.Subscribe()
	defer unsub()
	hub := NewHub(quietLogger())
	go hub.Run(ctx, events)

	authSvc := auth.NewService(auth.NewMemoryUserStore(), auth.NewMemorySessionStore(), time.Hour)
	site, err := New(Deps{
		Auth: authSvc, Stream: stream.NewHandler(streamSvc), Hub: hub,
		Ready: func(context.Context) error {
			if streamSvc.Snapshot() == nil {
				return io.ErrUnexpectedEOF
			}
			return nil
		},
		SessionTTL: time.Hour, Logger: quietLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Recover(quietLogger())(Logging(quietLogger())(http.NewCrossOriginProtection().Handler(site.Handler()))))
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) *http.Response {
		t.Helper()
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	body := func(resp *http.Response) string {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	// Esperar a que el stream publique su primer snapshot.
	for i := 0; i < 100; i++ {
		if resp := get("/healthz"); resp.StatusCode == 200 {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Sin sesión: player redirige, stream 401.
	if resp := get("/player"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("player sin sesión: %d", resp.StatusCode)
	}
	if resp := get("/stream/playlist.m3u8"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stream sin sesión: %d", resp.StatusCode)
	}

	// Registro (mismo origen: pasa la protección CSRF).
	form := url.Values{"name": {"Ana"}, "email": {"ana@example.com"}, "password": {"secreto123"}}
	resp, err := client.PostForm(srv.URL+"/register", form)
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("registro: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	if resp := get("/player"); resp.StatusCode != 200 {
		t.Fatalf("player con sesión: %d", resp.StatusCode)
	}
	if b := body(get("/stream/playlist.m3u8")); !strings.Contains(b, "#EXT-X-MEDIA-SEQUENCE:0") || !strings.Contains(b, "s0.ts") {
		t.Fatalf("playlist: %q", b)
	}
	if b := body(get("/stream/s0.ts")); b != "video s0.ts" {
		t.Fatalf("segmento: %q", b)
	}
	if resp := get("/stream/no-existe.ts"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("fuera de ventana: %d", resp.StatusCode)
	}

	// Logout y verificación.
	resp, _ = client.PostForm(srv.URL+"/logout", nil)
	resp.Body.Close()
	if resp := get("/player"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("player tras logout: %d", resp.StatusCode)
	}
	if resp := get("/stream/playlist.m3u8"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stream tras logout: %d", resp.StatusCode)
	}
}
```

(importar también `"net/http/httptest"`).

- [ ] **Step 2: Ejecutar**

Run: `go test -race ./internal/web/ -run TestE2E -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/web/e2e_test.go
git commit -m "test: prueba end-to-end de registro, player, stream protegido y logout"
```

---

### Task 23: Dockerfile y docker-compose

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `docker-compose.yml`
- Modify: `docker-compose.dev.yml` (agregar servicio `app` con build y volumen)

- [ ] **Step 1: `.dockerignore`**

```
.git
docs
dist
*.md
*.tar
*.zip
.env
```

- [ ] **Step 2: `Dockerfile`**

```dockerfile
# Etapa de compilación
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Imagen final
FROM alpine:3.21
RUN adduser -D -u 10001 app
# Los segmentos van en una capa anterior al binario: cambiar código no los re-copia.
COPY --chown=app:app segments/ /data/segments/
COPY --from=build /out/server /usr/local/bin/server
USER app
ENV PORT=8080 SEGMENTS_DIR=/data/segments
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1
ENTRYPOINT ["server"]
```

- [ ] **Step 3: `docker-compose.yml` (entrega)**

```yaml
services:
  app:
    image: prueba-zapping:latest
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://zapping:zapping@db:5432/zapping?sslmode=disable
      LOG_LEVEL: info
    depends_on:
      db:
        condition: service_healthy
    restart: unless-stopped

  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: zapping
      POSTGRES_PASSWORD: zapping
      POSTGRES_DB: zapping
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U zapping -d zapping"]
      interval: 5s
      timeout: 3s
      retries: 10
    restart: unless-stopped

volumes:
  pgdata:
```

- [ ] **Step 4: Completar `docker-compose.dev.yml`**

```yaml
services:
  app:
    build: .
    image: prueba-zapping:latest
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://zapping:zapping@db:5432/zapping?sslmode=disable
      LOG_LEVEL: debug
    volumes:
      - ./segments:/data/segments:ro
    depends_on:
      db:
        condition: service_healthy

  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: zapping
      POSTGRES_PASSWORD: zapping
      POSTGRES_DB: zapping
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U zapping -d zapping"]
      interval: 5s
      timeout: 3s
      retries: 10
```

- [ ] **Step 5: Verificar**

```bash
docker build -t prueba-zapping:latest .
docker compose up -d
sleep 20 && curl -i http://localhost:8080/healthz
docker compose ps      # app: healthy
docker compose down
```

Expected: `200 ok`; `docker images prueba-zapping` ≈ 500 MB.

- [ ] **Step 6: Commit**

```bash
git add Dockerfile .dockerignore docker-compose.yml docker-compose.dev.yml
git commit -m "feat: Dockerfile multi-stage y docker-compose de entrega y desarrollo"
```

---

### Task 24: README, INSTALACION y cierre del registro de decisiones

**Files:**
- Create: `README.md`, `INSTALACION.md`
- Modify: `docs/DECISIONES.md`

- [ ] **Step 1: Escribir `README.md`** con estas secciones (contenido real, no índices):

1. **Qué es**: una frase + captura (opcional) del player.
2. **Arquitectura**: el diagrama de paquetes de la spec §3 y las reglas de dependencia; link a `docs/superpowers/specs/…` y `docs/DECISIONES.md`.
3. **Cómo funciona el livestream**: reloj virtual (`publishAt(n)`), ventana de 3, discontinuidad en el cruce, `MEDIA-SEQUENCE`/`DISCONTINUITY-SEQUENCE`; ejemplo de playlist en el cruce (spec §4.3).
4. **Caché y concurrencia**: snapshot atómico, set de segmentos `[k-1, k+3]` con cota ≈ 66 MB, headers, caché de sesiones TTL 30s, SSE con descarte.
5. **Desviación del RFC 8216 §6.2.2** (D-11).
6. **Ejecutar en desarrollo**: `docker compose -f docker-compose.dev.yml up --build`; variables de entorno (tabla de la spec §8); `go test -race ./...`; tests de integración con `TEST_DATABASE_URL`.
7. **Prueba de carga**: obtener la cookie con `curl -c`, luego
   `go run github.com/rakyll/hey@latest -c 200 -z 30s -H "Cookie: session=<token>" http://localhost:8080/stream/playlist.m3u8` y pegar el resumen obtenido (requests/s, p99). Ejecutarla realmente y pegar los números.
8. **Entrega**: cómo generar `dist/prueba-zapping.tar` (`make docker-save` o los dos comandos `docker`), contenido del zip.

- [ ] **Step 2: Escribir `INSTALACION.md`** (va dentro del zip):

```markdown
# Instalación — Prueba Zapping

Requisitos: Docker 24+ con Docker Compose v2.

1. Cargar la imagen:
   docker load -i prueba-zapping.tar
2. Levantar la aplicación y su base de datos (desde la carpeta con docker-compose.yml):
   docker compose up -d
3. Esperar ~20 segundos y comprobar:
   curl http://localhost:8080/healthz   → ok
4. Abrir http://localhost:8080 en el navegador, crear una cuenta y entrar al player.
5. Detener:
   docker compose down        (agregar -v para borrar también la base de datos)

La imagen incluye los segmentos de video; la base de datos usa la imagen pública
postgres:17-alpine y sus datos persisten en el volumen `pgdata`.
Si el puerto 8080 está ocupado, cambiar el mapeo en docker-compose.yml (ej. "8081:8080").
```

- [ ] **Step 3: Cerrar el registro** en `docs/DECISIONES.md`: agregar P-n para cualquier problema encontrado en la ejecución del plan que no se haya registrado aún, y una entrada final "Estado: desarrollo completado el <fecha>" con el hash del último commit.

- [ ] **Step 4: Generar el entregable y verificarlo**

```bash
docker build -t prueba-zapping:latest .
mkdir -p dist && docker save prueba-zapping:latest -o dist/prueba-zapping.tar
cp docker-compose.yml INSTALACION.md dist/
# zip de dist/ (fuera del repo; dist/ está en .gitignore)
```

Seguir `INSTALACION.md` al pie de la letra en una carpeta limpia y confirmar que el player reproduce.

- [ ] **Step 5: Commit**

```bash
git add README.md INSTALACION.md docs/DECISIONES.md
git commit -m "docs: README, instructivo de instalación y cierre del registro de decisiones"
```

---

## Cobertura de la spec

| Spec | Tarea |
|---|---|
| §2 datos de entrada, N arbitrario | 3, 4 |
| §3 arquitectura y reglas de dependencia | 1, 16, 21 (verificables con `go list -deps ./internal/stream` sin paquetes internos) |
| §4.1–4.6 paquete `stream` | 3, 4, 5, 6, 7, 8 |
| §5 `auth` | 10, 11, 12, 13 |
| §6 `db` y esquema | 14, 15 |
| §7.1 rutas y middlewares | 16, 17, 18, 19, 21 |
| §7.2 vistas Neumorphism, panel | 16, 18, 20 |
| §7.3 SSE | 19, 20 |
| §8 configuración | 9 |
| §9 Docker y entrega | 2, 23, 24 |
| §10 errores y observabilidad | 7 (logs de tick), 21 (logging, recover, healthz) |
| §11 tests | en cada tarea; e2e en 22; carga en 24 |
