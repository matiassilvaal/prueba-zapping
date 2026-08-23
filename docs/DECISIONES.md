# Registro de decisiones, problemas y respuestas

Bitácora del desarrollo de la prueba técnica. Cada entrada registra el contexto,
las opciones consideradas, la decisión tomada y la razón, para poder auditar el
flujo completo al final. Las entradas se numeran de forma correlativa y nunca se
borran: si una decisión se revierte, se agrega una entrada nueva que la reemplaza.

Formato:

- **D-n**: decisión de diseño/arquitectura.
- **P-n**: problema o hallazgo encontrado durante el desarrollo.
- **Q-n**: pregunta abierta (se cierra referenciando la D-n que la resuelve).

---

## Decisiones

### D-1. Alcance del enunciado: manda el texto, no el ejemplo m3u8

- **Contexto**: el ejemplo m3u8 del enunciado muestra 4 segmentos de 6s; el texto exige 3 segmentos de 10s.
- **Decisión**: ventana de 3 segmentos, `EXT-X-TARGETDURATION:10`, rotación de un segmento por tick.
- **Razón**: el ejemplo es ilustrativo (confirmado por Matías, 2026-08-21).

### D-2. Un solo binario con paquetes independientes

- **Opciones**: (a) un binario/contenedor con paquetes separados; (b) dos binarios (`web` + `streamer`) con docker-compose.
- **Decisión**: (a). El paquete de streaming no debe importar nada de la web ni depender de ella; debe poder extraerse a su propio servicio sin modificarlo.
- **Razón**: entrega más simple ("un docker con el aplicativo funcionando") sin sacrificar separación.

### D-3. Base de datos: PostgreSQL

- **Opciones**: SQLite embebido vs PostgreSQL.
- **Decisión**: PostgreSQL.
- **Razón**: mantenibilidad y soporte de muchos usuarios concurrentes.

### D-4. Todo en Go, sin Node

- **Contexto**: el enunciado menciona "Livestreaming generado en NodeJS" (texto heredado de otra versión de la prueba).
- **Decisión**: backend y servidor de vistas en Go (`html/template`, `embed`), player con HLS.js. Sin restricción de CDN.

### D-5. Proteger todo el stream con sesión server-side

- **Decisión**: página del player, playlist `.m3u8`, segmentos `.ts` y SSE exigen sesión válida. Sesión por cookie `HttpOnly` con estado en servidor (no JWT).
- **Razón**: si solo se protege el HTML, cualquiera con la URL consume el stream sin cuenta.

### D-6. Stack: biblioteca estándar

- **Decisión**: `net/http` con el router de Go >= 1.22, sin framework. Dependencias mínimas (driver Postgres, bcrypt).

### D-7. Convenciones de código y commits

- Identificadores (paquetes, funciones, variables) en **inglés**, siguiendo convenciones de Go.
- Comentarios y textos para el usuario en **español**.
- Commits atómicos con prefijo `feat:`, `fix:`, `test:`, `docs:`, `chore:`, `refactor:` y mensaje en español.
- Formato de comentario de función:
  ```go
  // Qué hace la función
  //
  // @param [Type] var: descripción
  //
  // @return [Type] descripción
  ```

- Flujo de trabajo: ver D-18.

### D-8. Repositorio y entrega

- Repo público en GitHub: `prueba-zapping`. Entrega adicional: zip con la imagen Docker.
- TDD: los tests los escribe Claude por su cuenta; consulta solo ante dudas o problemas.

### D-9. Loop infinito: reloj virtual determinista (opción A1)

- **Contexto**: 64 segmentos (`segment0.ts`…`segment63.ts`), 63 de 10s y el último de 4.566667s. Ver P-1 y P-2.
- **Opciones**:
  - A1: la ventana se calcula como función pura de `(instante de arranque, ahora)` sobre la línea de tiempo acumulada de duraciones del manifiesto. El tick es la duración del segmento que sale (10s normalmente, 4.566667s una vez por vuelta).
  - A2: descartar `segment63` y usar ticker fijo de 10s.
- **Decisión**: A1.
- **Razón**: sin deriva entre reloj de pared y reloj de medios (A2 con ticker fijo acumularía 5.4s de atraso por vuelta y terminaría rebuffereando), reinicios coherentes, y testeable inyectando el reloj. Se conserva todo el contenido.

### D-10. Discontinuidad solo en el cruce 63 -> 0

- **Decisión**: `#EXT-X-DISCONTINUITY` se emite antes de `segment0.ts` cuando la ventana cruza el final; `#EXT-X-DISCONTINUITY-SEQUENCE` se incrementa cuando ese tag sale de la ventana (RFC 8216 §4.3.3.3). `EXT-X-MEDIA-SEQUENCE` crece monótonamente y nunca se reinicia.
- **Razón**: los PTS son continuos entre todos los segmentos (ver P-1); el único salto hacia atrás ocurre al dar la vuelta. El segmento corto no necesita discontinuidad: basta `#EXTINF:4.566667,` (TARGETDURATION es un máximo).

### D-11. Desviación consciente del RFC 8216 §6.2.2

- **Contexto**: el RFC pide que, al quitar un segmento, la duración restante de la playlist sea >= 3 x target duration (implica >= 4 segmentos en ventana). La prueba exige exactamente 3.
- **Decisión**: cumplir la prueba (3 segmentos) y documentar la desviación en el README. HLS.js lo reproduce correctamente.

### D-12. Segmentos dentro de la imagen, con override por volumen

- **Opciones**: copiar los 480 MB en la imagen vs montar volumen.
- **Decisión**: la imagen incluye los segmentos (funciona con un `docker run` sin pasos extra). El servicio lee la ruta desde la variable `SEGMENTS_DIR`, de modo que en desarrollo (`docker-compose`) se monta un volumen. El Dockerfile multi-stage copia `segments/` en una capa anterior al binario para que los cambios de código no re-copien 480 MB.
- **Repo**: los `.ts` van con Git LFS (ningún archivo supera los 100 MB, pero el total es 480 MB).

### D-13. Estrategia de caché y concurrencia

- **C1 Playlist**: el worker renderiza el `.m3u8` a `[]byte` una vez por tick y lo publica en un `atomic.Pointer` a un snapshot inmutable; los handlers solo leen el puntero (sin locks ni allocs). Headers `Cache-Control: no-cache` + `ETag` = secuencia de medios -> respuestas `304` a los clientes que repreguntan. El worker no conoce a los usuarios.
- **C2 Segmentos**: caché en RAM acotada a la ventana: los 3 activos + 1 de gracia (el recién removido sigue disponible, RFC §6.2.2) + prefetch del siguiente. Cota <= 5 x segmento más grande ~ 66 MB, evicción determinista en cada tick. Headers `Cache-Control: public, max-age=3600, immutable` + `ETag`. Descartado cachear los 480 MB completos.
- **C2-bis (corrección de Matías)**: **no hay fallback a disco**. Es un livestream: todos los clientes ven la misma ventana. Un segmento fuera de ventana + gracia responde `404`. El único acceso a disco lo hace el worker al precargar el siguiente segmento.
- **C3 Sesiones**: persistidas en Postgres (sobreviven reinicios, revocables) + caché en proceso con TTL corto (~30s) acotada en tamaño, para que el hot path del stream no consulte la DB en cada `.ts`. Logout invalida en DB y caché.
- **C4 SSE**: broadcaster en memoria, un canal con buffer 1 por cliente; si un cliente va lento se descarta el evento, nunca se bloquea al worker. Espectadores = conexiones SSE activas. Eventos por tick: secuencia, ventana, segundos al siguiente tick, espectadores.

### D-14. Extras (opcionales del enunciado)

- Contador de espectadores en vivo (SSE), número de secuencia actual, los 3 segmentos de la ventana y cuenta regresiva al siguiente tick.
- Frontend con estilo Neumorphism UI.

### D-15. Epoch del stream = arranque del proceso (cierra Q-1)

- **Contexto**: el loop 63 -> 0 es automático e infinito; el stream nunca termina ni requiere acción del usuario. La duda era únicamente qué pasa al reiniciar el proceso del servidor.
- **Opciones**: (a) epoch = instante de arranque del proceso; (b) variable opcional `STREAM_EPOCH` para secuencia continua entre reinicios/réplicas.
- **Decisión**: (a). Al reiniciar el servidor, el stream vuelve a `segment0` con `MEDIA-SEQUENCE:0`. Un navegador ya abierto ve retroceder la secuencia; HLS.js lo tolera y el SSE, al reconectarse, puede forzar recarga del player.
- **Razón**: no agrega configuración para un caso que Matías considera aceptable (el stream "comienza cuando arranca el servidor").

### D-16. Diseño completo aprobado (secciones 1-9)

- **Decisión**: Matías aprobó el 2026-08-21 las nueve secciones del diseño: estructura del repo, dominio puro (`Timeline`/`Window`), worker (`Service`), caché de segmentos, HTTP y rutas, auth y sesiones, base de datos, frontend/SSE, y Docker/config/tests.
- **Fuente de verdad**: `docs/superpowers/specs/2026-08-21-prueba-zapping-design.md`. Cualquier cambio posterior al diseño se registra aquí como nueva D-n y se refleja en la spec.

### D-17. Entrega con Postgres: imagen + docker-compose + instructivo

- **Contexto**: al usar Postgres (D-3), la imagen de la app por sí sola no funciona sin base de datos.
- **Decisión**: el zip de entrega contiene la imagen (`docker save`), el `docker-compose.yml` (que usa `postgres:17` público) y un `INSTALACION.md` que explica paso a paso cómo levantar el servidor (`docker load` + `docker compose up`). No se vuelve a SQLite.
- **Razón**: Matías prefiere Postgres por mantenibilidad y concurrencia; el costo es un paso documentado para el evaluador.

### D-18. Metodología de desarrollo: commits atómicos con revisión y aprobación mutua

Definida por Matías en el primer mensaje (2026-08-21). Aplica a toda la etapa de desarrollo, sin excepciones:

1. **Un commit = una funcionalidad pequeña.** Cada commit debe poder entenderse y revisarse solo, con su test (TDD: primero el test, luego la implementación mínima que lo hace pasar).
2. **Claude prepara el commit y se detiene.** Anuncia qué incluye, qué decisiones tomó y cómo verificarlo (comando de test y salida). No avanza al siguiente paso del plan.
3. **Tiempo de revisión de Matías.** Matías revisa el código con calma y puede modificarlo directamente.
4. **Revisión de Claude.** Claude revisa también el commit (y los cambios que Matías haya hecho), señala problemas o mejoras.
5. **Cambios.** Los ajustes derivados de la revisión los hace cualquiera de los dos; se incorporan al mismo commit (amend) o como `fix:` según lo acordado.
6. **Aprobación mutua explícita.** Solo cuando ambos dicen "aprobado" se continúa con el siguiente commit. Si uno no aprueba, se vuelve al punto 5.
7. Se repite hasta completar el plan de implementación.

Toda decisión, problema o respuesta que surja durante este ciclo se anota en este archivo (D-n / P-n / Q-n) para la auditoría final de Matías.

### D-19. Plan de implementación en 24 commits atómicos

- **Fuente**: `docs/superpowers/plans/2026-08-21-prueba-zapping.md`. Cada tarea = un commit con su test (TDD) y sigue el ciclo de D-18.
- **Orden**: base y LFS (1-2) → paquete `stream` (3-8) → config (9) → `auth` (10-13) → `db` (14-15) → `web` (16-20) → composición y middlewares (21) → e2e (22) → Docker (23) → documentación y entrega (24).
- **Decisiones menores tomadas al planificar**:
  - Módulo Go `prueba-zapping` (sin dominio): evita depender del usuario de GitHub y es válido para paquetes `internal/`.
  - Los tests de render de playlist usan strings esperados inline en lugar de golden files: más fáciles de revisar en el commit.
  - `POST /logout` no exige sesión: si no hay cookie, igual la borra y redirige a `/login` (comportamiento equivalente al de la spec).
  - El evento SSE `window` incluye `secondsToNextTick` además de `nextTickAt`, para que la cuenta regresiva no dependa de la sincronía de relojes cliente/servidor.
  - `make` no está instalado en la máquina de desarrollo (Windows); el Makefile queda para Linux/CI y el README documenta los comandos `go`/`docker` directos. La prueba de carga usa `go run github.com/rakyll/hey@latest`.

### D-20. Los segmentos de video no se versionan en GitHub (reemplaza la parte de repo de D-12)

- **Contexto**: D-12 proponía versionar los `.ts` con Git LFS. Al ejecutar la Tarea 2 del plan, Matías decidió (2026-08-23) que no es necesario: quien revisa ya tiene los segmentos y puede copiarlos a `segments/`; además el push habría consumido ~480 MB de la cuota LFS.
- **Decisión**: la carpeta `segments/` completa (incluido `segment.m3u8`, que es el catálogo de entrada con nombres y duraciones, no la playlist en vivo) queda fuera del repo e ignorada en `.gitignore`. Quien ejecute el código desde el repo debe aportar la carpeta `segments/` provista. La imagen Docker sí incluye los segmentos (esa parte de D-12 sigue vigente). `Prueba.md` (el enunciado) tampoco va al repo.
- **Aclaración conceptual** (duda de Matías): `segments/segment.m3u8` es input estático que el servidor lee una vez al arrancar; la playlist en vivo (3 segmentos, MEDIA-SEQUENCE, DISCONTINUITY) se genera en memoria en cada tick y se sirve en `/stream/playlist.m3u8`. El archivo original nunca se modifica.
- **Impacto**: el commit LFS de la Tarea 2 se deshizo antes de existir en ningún remoto; se eliminó la configuración y los objetos LFS locales. El README (Tarea 24) documentará: "copiar la carpeta `segments/` provista antes de ejecutar o construir la imagen". Los tests que usan el manifiesto real se saltan si la carpeta no está. La Tarea 2 del plan queda reemplazada por este commit.

---

## Problemas y hallazgos

### P-1. Los segmentos son un único stream continuo

- `ffprobe` (2026-08-21): H.264 + AAC. `segment0.ts` inicia en PTS 1.41s, `segment1.ts` en 11.42s, `segment63.ts` en 631.41s (duración 4.59s). Los timestamps son contiguos; el salto hacia atrás ocurre solo al volver de `segment63` a `segment0`. Resuelto por D-10.

### P-2. El último segmento dura 4.566667s

- El manifiesto original (`segments/segment.m3u8`, VOD con `EXT-X-ENDLIST`) declara `#EXTINF:4.566667,` para `segment63.ts`. Un ticker fijo de 10s desincronizaría reloj de pared y de medios. Resuelto por D-9.

### P-3. Tamaño de los segmentos

- 480 MB totales; el mayor ronda 13 MB (`segment1.ts`), el menor ~2 MB (`segment63.ts`). Condiciona D-12 y la cota de RAM de D-13.

### P-4. Sin compilador C en Windows: `-race` se ejecuta en Docker

- La máquina de desarrollo no tiene gcc/clang y el race detector de Go requiere cgo. `go test -race` falla nativo.
- **Solución adoptada (2026-08-23)**: ciclo rápido local con `go test ./...` (sin `-race`) y, antes de cerrar cada tarea, `go test -race ./...` dentro del contenedor `golang:1.26` con el código montado y un volumen `go-cache-zapping` para la caché de módulos:
  `docker run --rm -v "<repo>:/src" -w /src -v go-cache-zapping:/go golang:1.26 go test -race ./...`
- Docker Desktop debe estar levantado; el primer arranque en frío tardó varios minutos y hubo que reiniciar el engine una vez.

---

## Preguntas abiertas y respuestas

### Q-1. ¿Qué pasa con la secuencia al reiniciar el servidor?

- Resuelta por D-15.

### Q-2. ¿Funciona el sistema si se pasa a 75 segmentos (u otra cantidad)?

- **Respuesta**: sí, sin cambios de código. `N` y las duraciones se obtienen al parsear `segment.m3u8` en el arranque; toda la aritmética (`n % N`, acumulados, `TARGETDURATION` = techo de la duración máxima) trabaja sobre esa lista.
- **Supuestos que sí quedan fijos**: (1) `N >= 3` (tamaño de ventana), validado al arrancar; (2) los segmentos son PTS-continuos entre sí, de modo que la única discontinuidad es el cruce final -> inicio. Mezclar clips de distinto origen requeriría discontinuidad entre cada par y **no** está contemplado.

### Q-3. ¿Cómo se ve la playlist en vivo en distintos momentos? (ejemplos con los 64 segmentos reales)

Tomando `t` como segundos desde el epoch (arranque del proceso). El player solo ve estas ventanas de 3; nunca ve el catálogo completo.

**t = 0 s, secuencia 0 (arranque):**

```m3u8
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-DISCONTINUITY-SEQUENCE:0
#EXTINF:10.000000,
segment0.ts
#EXTINF:10.000000,
segment1.ts
#EXTINF:10.000000,
segment2.ts
```

**t = 10 s, secuencia 1:** salió `segment0`, entró `segment3`.

```m3u8
#EXT-X-MEDIA-SEQUENCE:1
#EXT-X-DISCONTINUITY-SEQUENCE:0
#EXTINF:10.000000,
segment1.ts
#EXTINF:10.000000,
segment2.ts
#EXTINF:10.000000,
segment3.ts
```

**t = 620 s, secuencia 62:** el último segmento es corto y ya asoma el cruce con su tag.

```m3u8
#EXT-X-MEDIA-SEQUENCE:62
#EXT-X-DISCONTINUITY-SEQUENCE:0
#EXTINF:10.000000,
segment62.ts
#EXTINF:4.566667,
segment63.ts
#EXT-X-DISCONTINUITY
#EXTINF:10.000000,
segment0.ts
```

**t = 630 s, secuencia 63:** segmento corto al frente; este tick dura solo 4.566667 s.

```m3u8
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

**t = 634.57 s, secuencia 64:** ya dio la vuelta. `segment0` va al frente con el tag delante; el tag sigue en la playlist, así que `DISCONTINUITY-SEQUENCE` aún es 0.

```m3u8
#EXT-X-MEDIA-SEQUENCE:64
#EXT-X-DISCONTINUITY-SEQUENCE:0
#EXT-X-DISCONTINUITY
#EXTINF:10.000000,
segment0.ts
#EXTINF:10.000000,
segment1.ts
#EXTINF:10.000000,
segment2.ts
```

**t = 644.57 s, secuencia 65:** el tag salió de la ventana → `DISCONTINUITY-SEQUENCE` pasa a 1 (RFC 8216 §4.3.3.3). `MEDIA-SEQUENCE` sigue creciendo, nunca vuelve a 0.

```m3u8
#EXT-X-MEDIA-SEQUENCE:65
#EXT-X-DISCONTINUITY-SEQUENCE:1
#EXTINF:10.000000,
segment1.ts
#EXTINF:10.000000,
segment2.ts
#EXTINF:10.000000,
segment3.ts
```

En la segunda vuelta el patrón se repite con secuencias 126-129 y `DISCONTINUITY-SEQUENCE` pasando de 1 a 2, indefinidamente. (Las cabeceras `#EXTM3U`, `#EXT-X-VERSION:3` y `#EXT-X-TARGETDURATION:10` se omiten en los ejemplos intermedios por brevedad; siempre están.)

### Q-4. ¿Para qué hay un segmento de gracia y uno de prefetch en la caché? (Tarea 6)

Para la secuencia `k`, la caché en RAM contiene 5 archivos:

| n | rol | por qué |
|---|---|---|
| k-1 | gracia | clientes que aún tienen la playlist anterior |
| k, k+1, k+2 | ventana | lo que la playlist anuncia |
| k+3 | prefetch | entra en el próximo tick sin tocar disco |

**Gracia (k-1): evitar 404 a clientes un tick atrasados.** El player no descarga playlist y segmentos en el mismo instante:

1. t = 19.9 s: HLS.js pide la playlist y recibe `[seg1, seg2, seg3]`.
2. t = 20.0 s: tick; la ventana pasa a `[seg2, seg3, seg4]`.
3. t = 20.1 s: HLS.js pide `seg1.ts`, que acaba de leer en la playlist. Sin gracia responderíamos 404 y el player entra en error fatal de red.

Con muchos usuarios haciendo polling a distinto ritmo, la carrera ocurre constantemente. El RFC 8216 §6.2.2 exige que un segmento removido siga disponible "duración del segmento + duración de la playlist más larga" (>= 10 s + 30 s). Se eligió conservar **1** segmento (10 s) porque HLS.js pide el segmento milisegundos después de leer la playlist, y así la cota de RAM se mantiene baja. Ser 100 % estricto con el RFC implicaría 4 de gracia (~105 MB); es cambiar un número en `cacheNames` si se decide.

**Prefetch (k+3): que el tick nunca dependa del disco.** Sin prefetch, en cada tick el worker leería del disco el segmento que entra (hasta 13 MB) **antes** de publicar el snapshot; con I/O lento el tick se atrasa y los clientes agotan su buffer. Con prefetch, en el tick `k` ya se carga `k+3`, que entrará en la ventana **en el tick siguiente**: cuando llega ese momento, publicar el snapshot es un swap de punteros sin I/O. La lectura de disco ocurre 10 s antes de que alguien la necesite. Beneficio extra: en el arranque se cargan 4 archivos antes del primer snapshot, verificando de paso el pipeline de carga.

Cota resultante: 5 x 13 MB ~ 66 MB fijos, independiente del número de usuarios.
