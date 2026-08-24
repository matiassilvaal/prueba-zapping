# Prueba Zapping — Livestreaming HLS simulado en Go

Servicio en Go que genera un livestreaming HLS a partir de segmentos de video
pregrabados, con registro/login de usuarios en PostgreSQL y un player web
protegido. Solo usuarios registrados pueden ver el stream. Se entrega como
imagen Docker + `docker-compose.yml` (ver `INSTALACION.md`).

- Documentación de diseño: `docs/superpowers/specs/2026-08-21-prueba-zapping-design.md`
- Bitácora de decisiones, problemas y respuestas: `docs/DECISIONES.md`

## Arquitectura

Un solo binario (`cmd/server`) compone paquetes independientes:

```
cmd/server          composición: config -> db -> stream -> web -> http.Server
internal/config     variables de entorno, validación, defaults
internal/stream     el "microservicio" HLS; solo depende de la stdlib
internal/auth       usuarios, sesiones server-side, middleware
internal/db         pool pgx, migraciones embebidas, stores Postgres
internal/web        páginas html/template, assets embebidos, SSE
migrations/         SQL versionado, embebido en el binario
```

Reglas de dependencia: `stream` no importa `auth`, `web` ni `db`; expone
`Snapshot()`, `Segment(name)`, `Subscribe()` y un `http.Handler`. La
autenticación envuelve al handler del stream **desde fuera** (en `cmd/server`),
de modo que el generador del livestream no sabe que existen usuarios: extraerlo
a su propio contenedor es copiar el paquete y un `main` mínimo.

## Cómo funciona el livestream

El manifiesto fuente (`segments/segment.m3u8`) se parsea una vez al arrancar:
nombres y duraciones (63 segmentos de 10 s + 1 de 4.566667 s en los datos
provistos; el código funciona con cualquier N >= 3). Sobre esa lista se define
un **reloj virtual**: el segmento global `n` (que crece sin tope; el archivo es
`n % N`) se publica en el instante `publishAt(n) = (n / N)·total + inicio(n % N)`.

La ventana vigente es una **función pura** de `(epoch, ahora)`: sin estado
mutable, sin deriva acumulada, con reinicios coherentes y testeable con un
reloj falso. Cada tick dura lo que dura el segmento que sale (10 s normalmente,
4.566667 s una vez por vuelta), así el reloj de medios y el de pared nunca se
desincronizan y el stream continúa indefinidamente.

- `EXT-X-MEDIA-SEQUENCE` crece en 1 por cada segmento removido y nunca se reinicia.
- Al dar la vuelta (último → primero) los PTS saltan hacia atrás, por eso se
  emite `#EXT-X-DISCONTINUITY` antes del primer segmento, y cuando ese tag sale
  de la ventana se incrementa `#EXT-X-DISCONTINUITY-SEQUENCE` ([RFC 8216, sección 4.3.3.3](https://datatracker.ietf.org/doc/html/rfc8216#section-4.3.3.3)).

Ejemplo de la playlist en el cruce (secuencia 63, tick corto):

```m3u8
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:63
#EXT-X-DISCONTINUITY-SEQUENCE:0
#EXTINF:4.566667,
segment63.ts
#EXT-X-DISCONTINUITY
#EXTINF:10.000000,
segment0.ts
#EXTINF:10.000000,
segment1.ts
```

El worker (una goroutine) no conoce a los usuarios: publica cada tick un
snapshot inmutable y duerme hasta el próximo. La concurrencia de usuarios la
maneja exclusivamente el servidor HTTP.

## Caché y concurrencia

- **Playlist**: se renderiza una vez por tick y se publica en un
  `atomic.Pointer`; cada request la lee sin locks ni allocs. `Cache-Control:
  private, no-cache` + `ETag` (= secuencia) para respuestas `304` (`private`:
  el recurso exige sesión y no debe quedar en cachés compartidas).
- **Segmentos**: caché en RAM acotada a `[k-1, k+3]` — 1 de gracia (el recién
  removido sigue disponible para clientes con la playlist anterior), la ventana
  de 3, y 1 de prefetch **best-effort**: el tick nunca espera al disco y, si esa
  lectura falla, la ventana se publica igual y se reintenta al tick siguiente.
  Cota fija ~66 MB, independiente del número de usuarios. Fuera de ese set →
  `404`: es un livestream, todos ven la misma ventana. Headers `private,
  max-age=3600, immutable` + `ETag`.
- **Sesiones**: cookie `HttpOnly` con token aleatorio; en la DB solo se guarda
  su SHA-256. Caché en proceso con TTL 30 s y tope de entradas: validar cada
  request del stream cuesta un hash + una lectura de mapa, no una consulta SQL.
  El `Max-Age` de la cookie y el `expires_at` de la sesión salen de la misma
  fuente (`auth.Service.TTL()`).
- **SSE**: hub con envío no bloqueante por cliente (los lentos pierden eventos,
  jamás frenan al worker). Los eventos `viewers` se coalescen (~250 ms) para
  que una ráfaga de conexiones no desplace al evento `window`. Espectadores =
  conexiones SSE activas.
- **Assets estáticos**: embebidos en el binario y servidos con URLs versionadas
  por contenido (`?v=<hash>`) + `Cache-Control: immutable` de un año — caché
  agresiva con invalidación inmediata al desplegar.

## Desviación consciente del RFC 8216

La [sección 6.2.2](https://datatracker.ietf.org/doc/html/rfc8216#section-6.2.2) pide conservar >= 3 × target duration al remover un segmento (implica
ventana de 4); el enunciado exige exactamente 3 segmentos por playlist, y eso
es lo que se implementa. HLS.js lo reproduce sin problemas. El mismo RFC pide
mantener disponible el segmento removido: eso sí se cumple con el segmento de
gracia.

## Comportamiento ante fallas y apagado

- **Sesión vencida con el player abierto**: HLS.js y el canal SSE detectan el
  `401` y redirigen a `/login`; los errores de red reales reintentan con
  backoff de 2 s (nunca un bucle de requests sin salida).
- **Disco lento o segmento ilegible**: el prefetch fallido no interrumpe el
  stream; solo la falta de un segmento obligatorio (gracia o ventana) congela
  el tick, que se recupera solo en cuanto el archivo vuelve a leerse.
- **Apagado ordenado**: ante SIGTERM el hub cierra sus conexiones SSE, así
  `http.Server.Shutdown` (y `docker stop`) termina en milisegundos en lugar de
  agotar los 10 s de gracia.

## Ejecutar en desarrollo

Requisitos: Go 1.26+, Docker. Los comandos son para bash (Git Bash en Windows,
Linux o macOS). Antes de nada, copiar la carpeta `segments/` provista (los
`.ts` y `segment.m3u8`) a la raíz del repo — no se versiona (ver D-20).

```bash
# Base de datos de desarrollo (host 5433, para no chocar con un Postgres local)
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d db

# Servidor local
DATABASE_URL='postgres://zapping:zapping@localhost:5433/zapping?sslmode=disable' \
SEGMENTS_DIR=./segments go run ./cmd/server
# abrir http://localhost:8080

# Todo dentro de Docker (build + segmentos montados como volumen; el compose
# de desarrollo es un override del de entrega: solo declara las diferencias)
docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build -d
```

Variables de entorno (todas opcionales salvo `DATABASE_URL`):

| Variable | Default | Descripción |
|---|---|---|
| `PORT` | `8080` | puerto HTTP |
| `DATABASE_URL` | — | DSN de PostgreSQL (obligatoria) |
| `DB_MAX_CONNS` | `10` | tamaño del pool |
| `SEGMENTS_DIR` | `/data/segments` | carpeta con los `.ts` y el manifiesto |
| `SEGMENTS_MANIFEST` | `segment.m3u8` | manifiesto fuente dentro de `SEGMENTS_DIR` |
| `SESSION_TTL` | `24h` | duración de la sesión |
| `COOKIE_SECURE` | `false` | flag `Secure` de la cookie (activar detrás de TLS) |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |

### Tests

```bash
go test ./...                      # unitarios + e2e (sin dependencias externas)

# Integración con Postgres real (usa el contenedor de desarrollo)
TEST_DATABASE_URL='postgres://zapping:zapping@localhost:5433/zapping?sslmode=disable' go test ./...

# Con race detector (requiere cgo; en Windows sin gcc se corre dentro de Docker)
docker run --rm -v "$PWD:/src" -w /src golang:1.26 go test -race ./...
```

## Prueba de carga

Con la imagen de entrega corriendo (`docker compose up -d`), una sesión válida
y [hey](https://github.com/rakyll/hey):

```bash
go run github.com/rakyll/hey@latest -c 200 -z 30s \
  -H "Cookie: session=<token>" http://localhost:8080/stream/playlist.m3u8
```

Resultado real (2026-08-24, Docker Desktop sobre Windows, 200 conexiones
concurrentes durante 30 s):

```
Requests/sec: 9489.04
Total:        284 820 respuestas, todas 200
Latencia:     p50 19 ms · p90 30 ms · p95 36 ms · p99 53 ms · max 147 ms
```

El worker publicó sus ticks sin retraso durante toda la prueba: generar la
playlist no depende de cuántos clientes la pidan.

## Construir y empaquetar la entrega

La entrega incluye una imagen por arquitectura (amd64 y arm64); el Dockerfile
cross-compila desde el host (`--platform=$BUILDPLATFORM` + `GOARCH`), así que
construir la variante arm desde una máquina x86 no paga emulación del compilador.

```bash
# requiere ./segments presente
mkdir -p dist
docker buildx build --platform linux/amd64 -t prueba-zapping:latest --load .
docker save prueba-zapping:latest -o dist/prueba-zapping-amd64.tar
docker buildx build --platform linux/arm64 -t prueba-zapping:latest --load .
docker save prueba-zapping:latest -o dist/prueba-zapping-arm64.tar
cp docker-compose.yml INSTALACION.md dist/
# comprimir dist/ y enviar; el receptor sigue INSTALACION.md
```

Se entregan dos tars (y no un tar multi-arch único) porque `docker load` con el
store clásico de imágenes solo acepta imágenes de una plataforma; dos archivos
funcionan en cualquier Docker sin requisitos extra.
