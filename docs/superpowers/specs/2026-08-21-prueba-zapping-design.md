# Prueba Zapping — Diseño del servicio de livestreaming HLS

Fecha: 2026-08-21. Estado: aprobado (ver `docs/DECISIONES.md`, D-16).

> **Nota (2026-08-24)**: este documento refleja el diseño aprobado antes de la
> implementación y se conserva como registro histórico. Los ajustes posteriores
> (por ejemplo `Cache-Control: private` en el stream, assets versionados por
> `?v=<hash>`, prefetch best-effort, endurecimientos de D-24/D-25) están
> documentados en `docs/DECISIONES.md` (D-20 a D-25); ante una discrepancia,
> manda el registro de decisiones.

## 1. Objetivo

Servicio en Go que simula un livestreaming HLS a partir de segmentos de video
pregrabados, con sitio web de tres páginas (Crear cuenta, Login, Player) y
registro de usuarios en PostgreSQL. Solo usuarios autenticados acceden al
player, a la playlist, a los segmentos y al canal de eventos. Se entrega como
imagen Docker + `docker-compose.yml` + instructivo.

Requisitos funcionales del enunciado que este diseño cubre:

- Ventana de 3 segmentos (30s) por request a la playlist.
- Cada tick sale el primer segmento de la lista y entra uno nuevo al final.
- `EXT-X-MEDIA-SEQUENCE` crece en uno por cada segmento removido y nunca se reinicia.
- El stream continúa indefinidamente: al llegar al último segmento vuelve al primero.
- El worker que genera la playlist no conoce ni depende de los usuarios conectados.
- Buen manejo de caché, de memoria y de usuarios concurrentes en el servidor HTTP.

Extras: contador de espectadores en vivo (SSE), secuencia actual, segmentos de
la ventana y cuenta regresiva al próximo tick; UI con estilo Neumorphism.

## 2. Datos de entrada

`segments/segment.m3u8` (VOD, `EXT-X-ENDLIST`) lista 64 archivos
`segment0.ts`…`segment63.ts`: 63 de 10.000000s y el último de 4.566667s
(total 634.566667s). Son H.264 + AAC en MPEG-TS con PTS continuos entre todos
los archivos; el único salto de timestamps ocurre al pasar de `segment63` a
`segment0`. Tamaño total 480 MB; el mayor archivo ronda 13 MB.

El diseño no asume N = 64: cualquier manifiesto con N >= 3 segmentos
PTS-continuos funciona sin cambios de código.

## 3. Arquitectura

Un solo binario (`cmd/server`) que compone tres paquetes independientes:

```
cmd/server/main.go          composición: config -> db -> stream.Service -> http.Server
internal/config             variables de entorno, validación, defaults
internal/stream             el "microservicio" HLS; solo depende de stdlib
internal/auth               usuarios, sesiones, middleware; interfaces de stores
internal/db                 pool pgx, migraciones embebidas, stores Postgres
internal/web                páginas html/template, assets embebidos, SSE
migrations/                 SQL versionado (embebido en el binario)
segments/                   .ts + segment.m3u8 (Git LFS)
docs/                       DECISIONES.md, specs, plan
Dockerfile · docker-compose.yml · docker-compose.dev.yml · Makefile · README.md · INSTALACION.md
```

Reglas de dependencia:

- `stream` no importa `auth`, `web` ni `db`. Expone `Service` y un `http.Handler`.
- `web` y `auth` usan `stream` solo mediante `Snapshot()`, `Segment(name)` y `Subscribe()`.
- El middleware de sesión envuelve al handler de `stream` desde `cmd/server`; `stream` no sabe que existe autenticación. Extraer el streamer a su propio contenedor es copiar el paquete y un `main` de 20 líneas.

## 4. Paquete `stream`

### 4.1 Manifiesto (`manifest.go`)

`ParseManifest(r io.Reader) ([]Segment, error)` lee un m3u8 de medios y devuelve
`[]Segment{Name string; Duration time.Duration}` en orden. Acepta `#EXTINF` con
decimales, ignora `#EXT-X-ENDLIST` y tags desconocidos, y falla con error claro si
hay menos de 3 segmentos, una duración no positiva o un nombre con separadores de
ruta (`/`, `\`, `..`).

### 4.2 Línea de tiempo y ventana (`timeline.go`, funciones puras)

`NewTimeline(segments []Segment) *Timeline` precalcula duraciones acumuladas
`cum[i]` (inicio del archivo i dentro de una vuelta), `total` y
`targetDuration = ceil(max duración)`.

Índice global `n` (0, 1, 2, …, sin tope). Archivo = `segments[n % N]`.
Instante de publicación relativo al epoch: `publishAt(n) = (n / N)·total + cum[n % N]`.

```go
type Entry struct {
    Name          string
    Duration      time.Duration
    Discontinuity bool // true si n % N == 0 && n > 0 (cruce fin -> inicio)
}

type Window struct {
    MediaSequence         uint64    // k
    DiscontinuitySequence uint64    // floor((k-1)/N) si k >= 1; 0 si k == 0
    Entries               []Entry   // exactamente WindowSize (3) elementos: n = k, k+1, k+2
    NextTick              time.Time // epoch + publishAt(k+1)
}

func (t *Timeline) WindowAt(epoch, now time.Time) Window
```

`k` = mayor índice con `publishAt(k) <= now - epoch` (si `now < epoch`, k = 0).
Se calcula con división entera por `total` más búsqueda binaria en `cum`: O(log N),
sin acumular estado, sin deriva. Tick efectivo = `Duration(k)`: 10s normalmente,
4.566667s cuando sale `segment63`.

`DiscontinuitySequence` cuenta los tags `EXT-X-DISCONTINUITY` que ya salieron de
la ventana (RFC 8216 §4.3.3.3): el tag pertenece a la entrada con `n % N == 0`,
y sale cuando `k > n`.

### 4.3 Render de playlist (`playlist.go`, función pura)

`RenderPlaylist(w Window, targetDuration int) []byte` produce:

```
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:127
#EXT-X-DISCONTINUITY-SEQUENCE:1
#EXTINF:4.566667,
segment63.ts
#EXT-X-DISCONTINUITY
#EXTINF:10.000000,
segment0.ts
#EXTINF:10.000000,
segment1.ts
```

`EXT-X-DISCONTINUITY-SEQUENCE` se emite siempre (vale 0 al inicio). Duraciones con
seis decimales. Sin `EXT-X-ENDLIST` (es live). Se verifica con golden files.

Desviación consciente: RFC 8216 §6.2.2 pide que la playlist conserve >= 3 × target
duration tras remover un segmento (4 segmentos); el enunciado exige 3. Se documenta
en el README.

### 4.4 Caché de segmentos (`cache.go`)

`segmentSet` es un `map[string][]byte` inmutable publicado en un `atomic.Pointer`.
En cada tick el worker construye un set nuevo con exactamente los nombres de
`n ∈ [k-1, k+3]` (gracia + ventana + prefetch; módulo N), reutilizando los `[]byte`
ya cargados y leyendo del disco solo los que falten (en régimen, uno por tick). Los
`[]byte` evictados los libera el GC.

Cota de memoria: 5 × tamaño del mayor segmento (~66 MB con los datos actuales),
independiente del número de clientes. Nada en el path HTTP lee disco.

`Segment(name string) ([]byte, bool)`: lookup en el set actual; cualquier nombre
fuera del set devuelve `false` (→ 404). No hay fallback a disco: es un livestream y
todos los clientes ven la misma ventana.

### 4.5 Worker (`service.go`)

```go
type Clock interface {
    Now() time.Time
    After(d time.Duration) <-chan time.Time
}

type Snapshot struct {
    Window   Window
    Playlist []byte
    ETag     string // strconv.Quote(MediaSequence)
}

type Service struct { /* timeline, epoch, clock, loader, snapshot atomic.Pointer[Snapshot], set atomic.Pointer[segmentSet], subs */ }

func NewService(tl *Timeline, loader SegmentLoader, clock Clock, opts ...Option) *Service
func (s *Service) Run(ctx context.Context) error
func (s *Service) Snapshot() *Snapshot              // nil hasta el primer tick
func (s *Service) Segment(name string) ([]byte, bool)
func (s *Service) Subscribe() (<-chan Window, func()) // canal con buffer 1; eventos descartados si el cliente no consume
```

`SegmentLoader` es `func(name string) ([]byte, error)`; la implementación de
producción lee de `SEGMENTS_DIR`. En tests se inyecta un loader en memoria y un reloj
falso.

Ciclo de `Run` (una sola goroutine):

1. `w := tl.WindowAt(epoch, clock.Now())`.
2. Construir el set de segmentos `[k-1, k+3]`; cargar del disco los faltantes. Si una carga falla en el primer tick, `Run` devuelve el error y el proceso termina. Si falla en régimen, se registra con `slog.Error`, no se publica snapshot nuevo (los clientes siguen viendo el anterior) y se reintenta en el próximo tick.
3. Swap atómico del set y del snapshot (playlist renderizada + ETag).
4. Notificar `w` a los suscriptores sin bloquear (`select` con `default`).
5. Esperar hasta `w.NextTick` (`clock.After`) o `ctx.Done()`.

Epoch = `clock.Now()` al llamar a `Run` (D-15). Al reiniciar el proceso, el stream
vuelve a `segment0` con `MEDIA-SEQUENCE:0`.

### 4.6 Handler HTTP (`handler.go`)

`NewHandler(s *Service) http.Handler` con dos rutas relativas:

- `GET /playlist.m3u8`: si `Snapshot()` es nil → `503`. Si `If-None-Match` coincide con
  el ETag → `304`. Si no → `200`, `Content-Type: application/vnd.apple.mpegurl`,
  `Cache-Control: no-cache`, `ETag`, cuerpo = `Playlist`.
- `GET /{name}`: `Segment(name)`; si no existe → `404`. Si existe →
  `http.ServeContent(bytes.NewReader(b))` con `Content-Type: video/mp2t`,
  `Cache-Control: public, max-age=3600, immutable`, `ETag` = `"name"`. `ServeContent`
  aporta `Range`, `If-None-Match` y `Content-Length`.

Se monta en `cmd/server` bajo `/stream/` con `http.StripPrefix`, envuelto por el
middleware de sesión.

## 5. Paquete `auth`

### 5.1 Modelo

```go
type User struct { ID int64; Name, Email string; PasswordHash []byte; CreatedAt time.Time }
type Session struct { TokenHash []byte; UserID int64; ExpiresAt time.Time }

type UserStore interface {
    Create(ctx, name, email string, hash []byte) (User, error) // ErrEmailTaken
    FindByEmail(ctx, email string) (User, error)              // ErrNotFound
}
type SessionStore interface {
    Create(ctx, s Session) error
    Get(ctx, tokenHash []byte) (Session, error) // ErrNotFound; no devuelve expiradas
    Delete(ctx, tokenHash []byte) error
    DeleteExpired(ctx) (int64, error)
}
```

### 5.2 Registro y login

- Validación: nombre 1–100 caracteres, email con formato válido y normalizado a
  minúsculas, contraseña 8–72 bytes (límite de bcrypt).
- Hash bcrypt cost 12.
- Login: mensaje único "Credenciales inválidas". Si el email no existe se compara
  contra un hash dummy para igualar el tiempo de respuesta.
- Email duplicado: mensaje "Ya existe una cuenta con ese email" en el formulario.

### 5.3 Sesiones

- Token: 32 bytes de `crypto/rand`, codificado base64url en la cookie `session`
  (`HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` según `COOKIE_SECURE`, `Max-Age` =
  `SESSION_TTL`, default 24h).
- En DB se guarda `sha256(token)`, nunca el token.
- Caché en proceso (`SessionCache`): `map[[32]byte]cached{userID, expiresAt, cachedAt}`
  con `sync.RWMutex`, TTL 30s, máximo 10 000 entradas (si se supera, no cachea) y
  barrido de expiradas cada minuto. Hot path del stream: validar una cookie cuesta un
  hash SHA-256 y una lectura de mapa; la DB se consulta como máximo una vez cada 30s
  por sesión.
- Logout: `Delete` en DB y en caché.
- Job de limpieza: `DeleteExpired` cada hora.

### 5.4 Middleware

`RequireSession(next http.Handler, onFail Mode)` valida la cookie, inyecta el
`userID` en el contexto y, si falla, redirige a `/login` (modo página) o responde
`401` JSON (modo API: `/stream/*`, `/events`).

## 6. Paquete `db`

- `pgxpool` con `DATABASE_URL`; `MaxConns` configurable (default 10).
- Migraciones: archivos `migrations/NNNN_nombre.sql` embebidos; runner propio con
  tabla `schema_migrations(version, applied_at)` y lock de advisory para evitar
  carreras entre réplicas. Se aplican al arrancar.
- Esquema inicial:

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
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);
```

- Stores Postgres implementan `auth.UserStore` y `auth.SessionStore`.

## 7. Paquete `web`

### 7.1 Rutas (router de `net/http`, Go >= 1.22)

| Ruta | Sesión | Comportamiento |
|---|---|---|
| `GET /` | — | redirige a `/player` si hay sesión; si no, a `/login` |
| `GET /register` · `POST /register` | — | formulario; en éxito crea sesión y redirige a `/player` |
| `GET /login` · `POST /login` | — | formulario; en éxito crea sesión y redirige a `/player` |
| `POST /logout` | ✔ | invalida sesión, borra cookie, redirige a `/login` |
| `GET /player` | ✔ | página del player |
| `GET /stream/playlist.m3u8` · `GET /stream/{name}` | ✔ (401) | `stream.Handler` |
| `GET /events` | ✔ (401) | SSE |
| `GET /static/{path}` | — | assets embebidos, `Cache-Control: public, max-age=86400` |
| `GET /healthz` | — | `200` si hay snapshot; `503` si no |

Usuarios ya autenticados que visitan `/login` o `/register` son redirigidos a `/player`.

Middlewares globales (composición explícita en `cmd/server`): `recover` (500 +
log), logging `slog` (método, ruta, status, duración), `http.CrossOriginProtection`
para los POST.

`http.Server`: `ReadHeaderTimeout 5s`, `ReadTimeout 10s`, `IdleTimeout 120s`,
`WriteTimeout 0` (SSE). Apagado: `SIGTERM/SIGINT` → cancela contexto del worker y
del hub → `Shutdown` con 10s de gracia.

### 7.2 Vistas

`html/template` con layout base + `register.html`, `login.html`, `player.html`,
embebidas con `embed`. Textos en español. CSS propio con estilo Neumorphism (fondo
`#e0e5ec`, sombras dobles claro/oscuro, inputs hundidos, botones elevados, tarjeta
del player). JS vanilla; HLS.js desde CDN con fallback a HLS nativo
(`video.canPlayType('application/vnd.apple.mpegurl')`).

Panel "EN VIVO" del player:

- Espectadores conectados.
- `MEDIA-SEQUENCE` actual y `DISCONTINUITY-SEQUENCE`.
- Los 3 segmentos de la ventana (el que está por salir marcado).
- Cuenta regresiva al próximo tick: el servidor envía `nextTickAt` (RFC3339 con
  milisegundos); el cliente descuenta localmente cada 250ms.

### 7.3 SSE (`/events`)

Hub en `web`: una goroutine se suscribe a `stream.Subscribe()` y reenvía a los
clientes conectados (`map[chan []byte]struct{}` bajo mutex; canal por cliente con
buffer 4; si está lleno se descarta el evento). Espectadores = clientes registrados
en el hub.

Eventos (`data` en JSON):

- `window`: `{ "sequence", "discontinuitySequence", "segments": [{name, duration, discontinuity}], "nextTickAt", "viewers" }` — al conectar y en cada tick.
- `viewers`: `{ "viewers" }` — cuando alguien entra o sale.
- Comentario `: keepalive` cada 15s.

Cliente: `EventSource('/events')`. En `onopen` tras una desconexión, si la
secuencia recibida es menor que la última vista (reinicio del servidor, D-15), se
recarga la fuente en HLS.js.

## 8. Configuración (`internal/config`)

| Variable | Default | Descripción |
|---|---|---|
| `PORT` | `8080` | puerto HTTP |
| `DATABASE_URL` | (obligatoria) | DSN Postgres |
| `DB_MAX_CONNS` | `10` | tamaño del pool |
| `SEGMENTS_DIR` | `/data/segments` | carpeta con los `.ts` |
| `SEGMENTS_MANIFEST` | `segment.m3u8` | manifiesto fuente dentro de `SEGMENTS_DIR` |
| `SESSION_TTL` | `24h` | duración de sesión |
| `COOKIE_SECURE` | `false` | flag `Secure` de la cookie |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |

El proceso falla al arrancar con mensaje claro si falta `DATABASE_URL`, si no puede
leer el manifiesto o si falta algún archivo referenciado.

## 9. Docker y entrega

- `Dockerfile` multi-stage: `golang:1.26-alpine` compila con `CGO_ENABLED=0`,
  `-trimpath`, `-ldflags "-s -w"`; runtime `alpine` con usuario no-root;
  `COPY segments/ /data/segments/` en una capa anterior al binario; `HEALTHCHECK`
  con `wget -qO- http://localhost:8080/healthz`.
- `docker-compose.yml`: servicios `app` (imagen `prueba-zapping`) y `db`
  (`postgres:17-alpine` con healthcheck `pg_isready`); `app` depende de `db` con
  `condition: service_healthy`; volumen nombrado para datos de Postgres.
- `docker-compose.dev.yml`: override que compila desde el Dockerfile y monta
  `./segments` como volumen.
- Entregable: zip con `prueba-zapping.tar` (`docker save`), `docker-compose.yml` e
  `INSTALACION.md` (pasos: `docker load`, `docker compose up`, abrir
  `http://localhost:8080`).
- Git LFS para `segments/*.ts`.

## 10. Manejo de errores y observabilidad

- Errores de dominio tipados (`auth.ErrEmailTaken`, `auth.ErrNotFound`,
  `auth.ErrInvalidCredentials`); los handlers los traducen a mensajes de formulario
  o códigos HTTP. Errores inesperados → `500` genérico + log con `slog.Error`.
- Logs JSON con `log/slog`: arranque (N segmentos, duración total, epoch), cada
  tick a nivel `debug` (secuencia, ventana, bytes en caché), requests a nivel `info`.
- `/healthz` refleja si el worker publicó snapshot y si la DB responde a `Ping`.

## 11. Estrategia de tests (TDD, siempre con `-race`)

- `stream`: tabla de casos para `WindowAt` (k=0; tick normal; ventana que cruza
  63→0 con `Discontinuity` en la entrada correcta; `DiscontinuitySequence` pasa a 1
  al salir el tag; tick corto de 4.566667s; varias vueltas; `now < epoch`); golden
  files para `RenderPlaylist`; `ParseManifest` con manifiestos válidos e inválidos;
  `Service` con reloj falso y loader en memoria: primer snapshot, avance de ticks,
  contenido exacto del set (`[k-1, k+3]`), evicción, `404` fuera de ventana,
  notificación a suscriptores sin bloqueo; handler con `httptest` (`503`, `200`,
  `304`, `404`, headers).
- `auth`: validación de formularios, hash/verify, `SessionCache` (hit, miss, TTL,
  tope de tamaño), middleware (redirect vs 401) con stores en memoria.
- `db`: tests de stores y migraciones contra Postgres real, solo si existe
  `TEST_DATABASE_URL` (`t.Skip` en caso contrario); `docker-compose.dev.yml` lo
  provee.
- `web`: handlers de páginas con stores en memoria; hub SSE (conteo de espectadores,
  descarte de cliente lento).
- E2E con `httptest.Server` y stores en memoria: registro → login → player →
  playlist → segmento → nombre fuera de ventana `404` → logout → `401`.
- `Makefile`: `fmt`, `vet`, `test`, `run`, `docker-build`, `docker-save`.
- README: comando de carga rápida (`hey -c 200 -z 30s` contra `/stream/playlist.m3u8`
  con cookie válida) como evidencia de concurrencia.

## 12. Fuera de alcance

- Múltiples canales/calidades (master playlist), DRM, transcodificación.
- Secuencia continua entre reinicios del servidor (D-15).
- Discontinuidades entre segmentos de distinto origen (Q-2).
- Rate limiting de login, verificación de email, recuperación de contraseña.
