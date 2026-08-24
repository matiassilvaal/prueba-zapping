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

- **Decisión**: `#EXT-X-DISCONTINUITY` se emite antes de `segment0.ts` cuando la ventana cruza el final; `#EXT-X-DISCONTINUITY-SEQUENCE` se incrementa cuando ese tag sale de la ventana ([RFC 8216, sección 4.3.3.3](https://datatracker.ietf.org/doc/html/rfc8216#section-4.3.3.3)). `EXT-X-MEDIA-SEQUENCE` crece monótonamente y nunca se reinicia.
- **Razón**: los PTS son continuos entre todos los segmentos (ver P-1); el único salto hacia atrás ocurre al dar la vuelta. El segmento corto no necesita discontinuidad: basta `#EXTINF:4.566667,` (TARGETDURATION es un máximo).

### D-11. Desviación consciente del [RFC 8216, sección 6.2.2](https://datatracker.ietf.org/doc/html/rfc8216#section-6.2.2)

- **Contexto**: el RFC pide que, al quitar un segmento, la duración restante de la playlist sea >= 3 x target duration (implica >= 4 segmentos en ventana). La prueba exige exactamente 3.
- **Decisión**: cumplir la prueba (3 segmentos) y documentar la desviación en el README. HLS.js lo reproduce correctamente.

### D-12. Segmentos dentro de la imagen, con override por volumen

- **Opciones**: copiar los 480 MB en la imagen vs montar volumen.
- **Decisión**: la imagen incluye los segmentos (funciona con un `docker run` sin pasos extra). El servicio lee la ruta desde la variable `SEGMENTS_DIR`, de modo que en desarrollo (`docker-compose`) se monta un volumen. El Dockerfile multi-stage copia `segments/` en una capa anterior al binario para que los cambios de código no re-copien 480 MB.
- **Repo**: los `.ts` van con Git LFS (ningún archivo supera los 100 MB, pero el total es 480 MB).

### D-13. Estrategia de caché y concurrencia

- **C1 Playlist**: el worker renderiza el `.m3u8` a `[]byte` una vez por tick y lo publica en un `atomic.Pointer` a un snapshot inmutable; los handlers solo leen el puntero (sin locks ni allocs). Headers `Cache-Control: private, no-cache` + `ETag` = secuencia de medios -> respuestas `304` a los clientes que repreguntan. El worker no conoce a los usuarios.
- **C2 Segmentos**: caché en RAM acotada a la ventana: los 3 activos + 1 de gracia (el recién removido sigue disponible, [RFC 8216, sección 6.2.2](https://datatracker.ietf.org/doc/html/rfc8216#section-6.2.2)) + prefetch del siguiente. Cota <= 5 x segmento más grande ~ 66 MB, evicción determinista en cada tick. Headers `Cache-Control: private, max-age=3600, immutable` + `ETag` (privado: el recurso exige sesión y no debe quedar en cachés compartidas). Descartado cachear los 480 MB completos.
- **C2-bis (corrección de Matías)**: **no hay fallback a disco**. Es un livestream: todos los clientes ven la misma ventana. Un segmento fuera de ventana + gracia responde `404`. El único acceso a disco lo hace el worker al precargar el siguiente segmento.
- **C3 Sesiones**: persistidas en Postgres (sobreviven reinicios, revocables) + caché en proceso con TTL corto (~30s) acotada en tamaño, para que el hot path del stream no consulte la DB en cada `.ts`. Logout invalida en DB y caché.
- **C4 SSE**: broadcaster en memoria, un canal con buffer corto (4) por cliente; si un cliente va lento se descarta el evento, nunca se bloquea al worker. Los eventos `viewers` se coalescen (~250 ms) para que una ráfaga de altas/bajas no desplace al evento `window`. Espectadores = conexiones SSE activas. Eventos por tick: secuencia, ventana, segundos al siguiente tick, espectadores.

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
  - **Instrucciones del README (indicación de Matías, 2026-08-23)**: los comandos se documentan para **bash** en Windows (Git Bash), Linux y macOS; no se documenta PowerShell ni cmd. La sintaxis `VAR=valor comando` es válida en los tres.
  - **Cuenta regresiva del panel**: el refresco pasó de 250 ms a 100 ms (commit `32aa159`) tras observar Matías que el contador saltaba de a 0.2-0.3 s; 100 ms coincide con la resolución mostrada (un decimal).

### D-20. Los segmentos de video no se versionan en GitHub (reemplaza la parte de repo de D-12)

- **Contexto**: D-12 proponía versionar los `.ts` con Git LFS. Al ejecutar la Tarea 2 del plan, Matías decidió (2026-08-23) que no es necesario: quien revisa ya tiene los segmentos y puede copiarlos a `segments/`; además el push habría consumido ~480 MB de la cuota LFS.
- **Decisión**: la carpeta `segments/` completa (incluido `segment.m3u8`, que es el catálogo de entrada con nombres y duraciones, no la playlist en vivo) queda fuera del repo e ignorada en `.gitignore`. Quien ejecute el código desde el repo debe aportar la carpeta `segments/` provista. La imagen Docker sí incluye los segmentos (esa parte de D-12 sigue vigente). `Prueba.md` (el enunciado) tampoco va al repo.
- **Aclaración conceptual** (duda de Matías): `segments/segment.m3u8` es input estático que el servidor lee una vez al arrancar; la playlist en vivo (3 segmentos, MEDIA-SEQUENCE, DISCONTINUITY) se genera en memoria en cada tick y se sirve en `/stream/playlist.m3u8`. El archivo original nunca se modifica.
- **Impacto**: el commit LFS de la Tarea 2 se deshizo antes de existir en ningún remoto; se eliminó la configuración y los objetos LFS locales. El README (Tarea 24) documentará: "copiar la carpeta `segments/` provista antes de ejecutar o construir la imagen". Los tests que usan el manifiesto real se saltan si la carpeta no está. La Tarea 2 del plan queda reemplazada por este commit.

### D-21. HLS.js embebido en el binario, no desde CDN (ajusta D-4)

- **Contexto**: D-4 permitía cargar HLS.js desde un CDN. Al revisar la Tarea 20, Matías señaló que el enunciado exige tener HLS.js o Video.js en el proyecto, y que la entrega debe funcionar sin depender de terceros en tiempo de ejecución.
- **Decisión** (2026-08-23): `hls.min.js` v1.7.1 (Apache-2.0) se copia a `internal/web/static/vendor/` junto con su licencia y un README con versión y origen; se sirve desde `/static/vendor/hls.min.js` y viaja embebido en el binario vía `embed`. El test del player verifica tanto la referencia local como que el archivo se sirva.
- **Razón**: imagen 100 % autocontenida (funciona sin internet), versión fija y reproducible, sin riesgo por caídas o bloqueos del CDN. Costo: +600 KB en el binario.

### D-26. Entrega multi-arquitectura: un tar por plataforma (amd64 y arm64)

- **Contexto**: la imagen de entrega estaba construida solo para `linux/amd64`; en una máquina ARM (Apple Silicon) correría emulada o no correría. Pedido por Matías (2026-08-24).
- **Opciones**: (a) tar multi-arch único (requiere el store containerd tanto al generar como al cargar; el Docker del evaluador puede no tenerlo); (b) dos tars, uno por arquitectura, ambos con el tag `prueba-zapping:latest` para que el mismo `docker-compose.yml` sirva sin cambios.
- **Decisión**: (b), por compatibilidad con cualquier Docker. El Dockerfile adopta el patrón de cross-compilación (`FROM --platform=$BUILDPLATFORM` + `GOOS/GOARCH` de `TARGETOS/TARGETARCH`): el compilador corre nativo en el host y solo la imagen final es de la plataforma destino. `INSTALACION.md` indica qué tar cargar según `uname -m`.
- **Verificación**: el binario arm64 ejecuta bajo QEMU (falla con el error esperado de configuración, no de arquitectura); ambos tars ~500 MB en `dist/`.

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

### P-5. Postgres local en el puerto 5432: el contenedor de desarrollo usa 5433

- Al levantar `docker-compose.dev.yml` con `5432:5432`, los tests de integración fallaban con "la autentificación password falló para el usuario zapping": la máquina tiene un PostgreSQL local (12/14) escuchando en 5432 y las conexiones caían ahí en lugar del contenedor.
- **Solución (2026-08-23)**: el servicio `db` del compose de desarrollo se publica en el host como `5433:5432`. La URL de tests pasa a ser `postgres://zapping:zapping@localhost:5433/zapping?sslmode=disable`. Dentro del contenedor `golang:1.26` (para `-race`) se usa `host.docker.internal:5433`. El `docker-compose.yml` de entrega no expone el puerto de la DB al host, así que no le afecta.

### D-22. Cierre del desarrollo

- **Estado**: desarrollo completado el 2026-08-24. Las 24 tareas del plan (D-19) se ejecutaron bajo el ciclo D-18; la Tarea 2 fue reemplazada por D-20 y se agregaron dos commits fuera de plan a pedido de Matías: HLS.js embebido (D-21) y el ajuste de la cuenta regresiva a 100 ms.
- **Verificación final**: suite completa con `-race` en Docker en verde (5 paquetes), flujo end-to-end automatizado, imagen de entrega (524 MB) levantada con el compose de entrega (`healthy`, apagado ordenado con SIGTERM verificado) y prueba de carga real: 200 conexiones concurrentes durante 30 s contra la playlist → 9489 req/s, p99 = 53 ms, 284 820 respuestas 200, sin errores.
- **Entregables**: repo `prueba-zapping` (sin `segments/` ni `Prueba.md`, ver D-20) y zip con `prueba-zapping.tar` (docker save), `docker-compose.yml` e `INSTALACION.md`.

### D-23. Revisión integral posterior al cierre (2026-08-24)

- **Contexto**: con el desarrollo cerrado (D-22) se hizo una revisión completa del repositorio buscando bugs reales, código subóptimo y refactors — no casos borde imposibles. Se aplicó en 14 commits atómicos con TDD (test en rojo antes de cada fix); la suite completa quedó en verde.
- **Fixes de comportamiento**:
  - `Cache-Control: private` en playlist y segmentos: son recursos protegidos por sesión y `public` autorizaba a cachés compartidas (proxies/CDN) a servirlos a otros usuarios sin pasar por la autenticación (actualiza D-13 C1/C2).
  - Prefetch best-effort: un fallo al leer `k+3` ya no bloquea la publicación de la ventana `k`; solo la falta de un segmento obligatorio (gracia o ventana) detiene el tick, con recuperación automática (actualiza Q-4).
  - El hub SSE cierra sus clientes al cancelarse su contexto: antes `http.Server.Shutdown` esperaba los 10 s de gracia completos con un solo espectador conectado, compitiendo con el SIGKILL de `docker stop`.
  - Player ante `401` (sesión vencida o cerrada en otra pestaña): HLS.js y EventSource redirigen a `/login`; antes quedaban en un bucle de reintentos / "Reconectando…" sin salida. Los errores de red reales reintentan con backoff de 2 s.
  - Assets estáticos versionados por contenido (`?v=<hash>` + `immutable` de 1 año) y sin listado de directorios: antes, tras un despliegue, los navegadores podían seguir con el `player.js` viejo hasta 24 h.
  - Eventos `viewers` coalescidos (~250 ms) para que una ráfaga de altas/bajas no llene los buffers y desplace al evento `window` (actualiza D-13 C4).
  - `Recover` re-lanza `http.ErrAbortHandler` y no escribe "Error interno" sobre una respuesta ya iniciada (SSE); `Logging` registra 200 implícito; `healthz` responde `not ready` y loguea el detalle (los errores de pgx pueden incluir el DSN).
  - El hub se suscribe al stream **antes** de arrancar el worker: la primera ventana llega por orden de composición, no por el timing del primer tick.
- **Refactors y perf**:
  - TTL único: se eliminó `Deps.SessionTTL`; el `Max-Age` de la cookie y el `expires_at` de la sesión salen de `auth.Service.TTL()`.
  - El janitor de sesiones pasó de `main` a `auth.Service.RunJanitor` con períodos inyectables (y test).
  - Hash dummy de bcrypt precalculado en `NewService`: el primer login con email inexistente pagaba dos bcrypt (~0.5 s con costo 12). La caché de sesiones comparte el reloj del servicio.
  - `servePlaylist` sobre `http.ServeContent`: If-None-Match con listas (antes igualdad exacta), HEAD y Range uniformes con los segmentos.
  - Log de `publish` sin hardcodear `WindowSize` y sin construir datos con Debug apagado.
- **Build/entrega**:
  - `HEALTHCHECK` usa `${PORT}`: con `-e PORT=9000` el contenedor quedaba unhealthy con la app sana.
  - `docker-compose.dev.yml` pasa a ser un **override** del compose base: `docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build` (antes duplicaba el servicio `db` completo).
  - `go.mod` pide `go 1.26` en lugar del patch exacto 1.26.5 (evita descargas de toolchain en el build Docker); `.dockerignore` deja pasar el README de procedencia de HLS.js (D-21).
- **Tests agregados**: `/events` sin sesión → 401; descarte de eventos a clientes SSE lentos (prometido en la sección 11 de la spec); cancelación de suscripción del stream; `DB_MAX_CONNS`/`COOKIE_SECURE`/`PORT` inválidos en config; `web.New` con logger nil usa `slog.Default()`; el helper `readEvent` corta a los 3 s de verdad (antes un evento ausente colgaba el paquete entero).
- **Nota de auditoría**: el texto de D-13 (C1, C2, C4) y de Q-4 se actualizó en esas mismas entradas para reflejar el comportamiento vigente; esta entrada registra el porqué de cada cambio.

### D-24. Segunda revisión integral: endurecimiento (2026-08-24)

- **Contexto**: revisión completa del repositorio (independiente de D-23) buscando refactors, código subóptimo y bugs plausibles en operación normal. No se encontraron bugs críticos ni algoritmos O(N²) en rutas calientes; los hallazgos fueron de endurecimiento y se aplicaron en 10 commits atómicos con TDD (test en rojo antes de cada fix de comportamiento).
- **Endurecimiento**:
  - `healthz` detecta el stream estancado: `stream.Service.Ready()` exige snapshot publicado **y fresco** (tolerancia 2×`TARGETDURATION` sobre `NextTick`). Antes, si `publish` fallaba de forma persistente (volumen desmontado, archivo borrado) se conservaba el snapshot viejo para siempre y el healthcheck de Docker seguía en verde con los players congelados.
  - bcrypt acotado: semáforo con `GOMAXPROCS` cupos alrededor de `HashPassword`/`CheckPassword` (incluido el hash dummy). Con costo 12, cada POST inválido a `/login` o `/register` cuesta cientos de ms de CPU; sin tope, un bucle trivial saturaba los cores y facilitaba fuerza bruta. La espera respeta la cancelación del contexto. Se eligió el semáforo (y no un rate-limit por IP) por simplicidad: acota el daño total sin estado por cliente.
  - Deadline de escritura de 30 s por respuesta en playlist y segmentos vía `http.ResponseController` (el `statusWriter` ya exponía `Unwrap`). El servidor corre con `WriteTimeout: 0` por el SSE; sin esto, un cliente leyendo a goteo retenía conexión y goroutine sin límite.
  - `SessionCache.Put` barre las entradas vencidas antes de rechazar cuando la caché está llena: una caché llena de sesiones muertas dejaba a los logins nuevos pegando a Postgres hasta el próximo sweep periódico.
  - Migraciones con timeout de 30 s en el arranque (`pg_advisory_lock` espera indefinidamente si otra réplica quedó colgada); `parseExtInf` rechaza `NaN`/`Inf` explícitamente; índice único sobre `lower(email)` (migración `0002`) como cinturón y tirantes de la normalización en la app.
- **Refactors**: `Register`/`Login` devuelven cero valores junto a un error (antes filtraban el `User` con hash si `openSession` fallaba); condición muerta eliminada en `cacheNames`.
- **Tests agregados**: estancamiento y recuperación de `Ready`; semáforo de bcrypt (contexto vencido + liberación de cupos); sweep inline de la caché; duplicado de email con otras mayúsculas contra Postgres real; e2e de `/events` autenticado a través de `Recover → Logging → CSRF` leyendo el evento `window` inicial (fija que `statusWriter` preserva `Flush` en el flujo real).
- **Límite conocido y aceptado**: una conexión `/events` abierta sobrevive al logout (la autenticación es al conectar, comportamiento estándar de SSE). No expone video —solo metadatos de ventana y conteo de espectadores— y la conexión muere al recargar o navegar; no se agrega revalidación por evento.

### D-25. Tercera revisión integral (2026-08-24)

- **Contexto**: revisión completa del repositorio (independiente de D-23/D-24) con el mismo criterio: refactors, código subóptimo (O(N²) y similares) y casos plausibles en operación normal, no bordes imposibles. Veredicto del revisor: sin hallazgos críticos y listo para producción — el hot path ya es O(1) por request y `WindowAt` O(log N). Se aplicaron 1 hallazgo importante y 5 menores en 6 commits atómicos; suite completa en verde incluyendo los tests de integración contra Postgres real.
- **Fix importante**:
  - Deadline de escritura de 30 s **por evento** en el SSE (`hub.ServeHTTP`, vía `http.ResponseController`): D-24 lo aplicó a playlist/segmentos pero el hub seguía escribiendo eventos y keepalives sin deadline con `WriteTimeout: 0`. Un cliente que deja de leer sin cerrar el socket (móvil sin cobertura, laptop suspendida) llenaba el buffer TCP y dejaba `Write` bloqueado hasta agotar las retransmisiones del kernel (~15 min), reteniendo goroutine/conexión y demorando el `Shutdown`; ahora corta a los 30 s. Cada escritura exitosa empuja el deadline y el keepalive de 15 s mantiene viva una conexión sana.
- **Fixes menores**:
  - `FindByEmail` consulta `WHERE lower(email) = $1`: la lectura ahora usa el índice funcional de la migración `0002` (antes solo servía para la unicidad) y encuentra filas insertadas sin la normalización de la app. Pendiente para una migración futura: eliminar el `UNIQUE(email)` simple de `0001`, hoy redundante.
  - Logs de request de alto volumen y baja señal a Debug: los 2xx/3xx de `/healthz` (cada 10 s por el HEALTHCHECK) y de `/stream/*` (un segmento por espectador cada ~10 s) inundaban el log en Info; sus errores (>= 400) siguen en Info.
  - El constructor de `auth.Service` hace panic si `bcrypt.GenerateFromPassword` falla al precalcular el hash dummy (antes ignoraba el error): sin hash dummy, el login con email inexistente respondería al instante y delataría qué emails existen (oráculo de timing).
- **Refactors**: eliminado `auth.UserID` y la inyección del id de usuario al contexto (API muerta: solo la usaba su propio test); los tests E2E comparten el setup en `newE2EStack`/`registerUser` y ya no hacen nil-deref sobre `resp.StatusCode` cuando el request falla con `err != nil`.
- **Hallazgos registrados sin aplicar**: FK `sessions.user_id` sin índice (solo importa si algún día se borran usuarios: el `CASCADE` haría seq-scan); `Cache-Control: immutable` en segmentos supone que un redeploy no cambia el contenido manteniendo los nombres (si eso cambia, versionar la URL como los assets); recomendaciones para multi-réplica (un logout tarda hasta 30 s en propagarse por la caché de sesiones de otra réplica; `viewers` es por proceso), `COOKIE_SECURE=true` en el compose de entrega, métricas mínimas y rate-limit por IP en `/login` si se expone públicamente.

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

**t = 644.57 s, secuencia 65:** el tag salió de la ventana → `DISCONTINUITY-SEQUENCE` pasa a 1 ([RFC 8216, sección 4.3.3.3](https://datatracker.ietf.org/doc/html/rfc8216#section-4.3.3.3)). `MEDIA-SEQUENCE` sigue creciendo, nunca vuelve a 0.

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

Con muchos usuarios haciendo polling a distinto ritmo, la carrera ocurre constantemente. El [RFC 8216, sección 6.2.2](https://datatracker.ietf.org/doc/html/rfc8216#section-6.2.2) exige que un segmento removido siga disponible "duración del segmento + duración de la playlist más larga" (>= 10 s + 30 s). Se eligió conservar **1** segmento (10 s) porque HLS.js pide el segmento milisegundos después de leer la playlist, y así la cota de RAM se mantiene baja. Ser 100 % estricto con el RFC implicaría 4 de gracia (~105 MB); es cambiar un número en `cacheNames` si se decide.

**Prefetch (k+3): que el tick nunca dependa del disco.** Sin prefetch, en cada tick el worker leería del disco el segmento que entra (hasta 13 MB) **antes** de publicar el snapshot; con I/O lento el tick se atrasa y los clientes agotan su buffer. Con prefetch, en el tick `k` ya se carga `k+3`, que entrará en la ventana **en el tick siguiente**: cuando llega ese momento, publicar el snapshot es un swap de punteros sin I/O. La lectura de disco ocurre 10 s antes de que alguien la necesite. El prefetch es best-effort: si su lectura falla, la ventana se publica igual y se reintenta en el tick siguiente (solo la falta de un segmento obligatorio —gracia o ventana— bloquea el tick). Beneficio extra: en el arranque se cargan 4 archivos antes del primer snapshot, verificando de paso el pipeline de carga.

Cota resultante: 5 x 13 MB ~ 66 MB fijos, independiente del número de usuarios.

### Q-5. ¿Qué significa "lecturas lock-free" y cómo funcionan `Subscribe`/`broadcast`? (Tarea 7, `service.go`)

**1. Lecturas lock-free (`Snapshot()` y `Segment()`).**

- *Problema*: un escritor (el worker) y miles de lectores (una goroutine por request HTTP) sobre el mismo estado. Tocar la misma variable sin coordinación es una data race.
- *Solución clásica*: `RWMutex`. Funciona, pero cada request paga tomar/soltar el lock y, mientras el worker escribe, todos los lectores esperan. Con 1000 usuarios es un punto de contención.
- *Solución adoptada*: `atomic.Pointer` + inmutabilidad. Dos ideas juntas:
  1. El `*Snapshot` (ventana + playlist renderizada + ETag) se construye completo en privado y **nunca se modifica** después de publicado. Lo mismo el `segmentSet` (map nuevo por tick; los `[]byte` se reutilizan y tampoco se mutan).
  2. Lo único compartido es el puntero, que se lee/escribe con una instrucción atómica de CPU (`Load`/`Store`). Un lector obtiene el puntero viejo o el nuevo, nunca uno "a medias".
- *Por qué es seguro sin lock*: un lector que obtuvo el snapshot viejo puede seguir usándolo aunque el worker publique uno nuevo un microsegundo después; el viejo sigue en memoria (el GC lo libera cuando nadie lo referencia) y es inmutable. Analogía: cada tick imprime una edición completa de un periódico y cambia la pila del kiosco; quien ya tiene la edición anterior en la mano no se ve afectado.
- *Costo*: una lectura de puntero por request, sin espera ni contención, independiente del número de lectores.
- *Orden de los dos `Store` en `publish`*: primero el set de segmentos, después el snapshot de la playlist. Al revés, un cliente podría leer la playlist nueva antes de que sus archivos estén en caché y recibir un 404 espurio.

**2. `Subscribe` / `broadcast`.**

- Responde a otra pregunta: no "cuál es el estado ahora" sino "avísame cada vez que cambie". Lo usa el hub SSE para empujar eventos al navegador en el instante del tick, sin polling.
- `Subscribe()` crea un canal con buffer 1, lo registra en `subs` y devuelve el canal + una función de baja.
- `broadcast(w)` (llamado al final de cada `publish`) recorre los canales con `select { case ch <- w: default: }`. Un envío normal bloquearía hasta que el suscriptor lea; si un suscriptor se colgó o es lento, el worker se quedaría esperándolo y el livestream se congelaría para todos, justo lo que el enunciado prohíbe. Con `default` el envío es instantáneo: si el suscriptor no consumió el evento anterior, pierde este. No importa, porque cada evento trae la ventana completa (no un delta).
- *Por qué buffer 1*: con buffer 0 el envío solo tendría éxito si el suscriptor está ya bloqueado leyendo en ese instante; cualquier suscriptor ocupado perdería el evento. Con buffer 1 el evento queda guardado hasta que termine lo que estaba haciendo. Más buffer solo acumularía eventos viejos.
- El mutex `s.mu` protege únicamente el mapa de suscriptores (cambia al conectar/desconectar un SSE), no el hot path de los requests.

| Mecanismo | Quién lo usa | Pregunta que responde | Costo por operación |
|---|---|---|---|
| `atomic.Pointer` (snapshot, set) | handlers HTTP, miles de veces por segundo | "¿cuál es el estado ahora?" | una lectura de puntero, sin lock |
| `Subscribe`/`broadcast` (canales) | hub SSE, pocos suscriptores | "avísame cuando cambie" | un envío no bloqueante por suscriptor, una vez por tick |

El test `TestService_SuscriptorLentoNoBloquea` verifica exactamente esto: un suscriptor que nunca lee termina con 1 evento en el buffer mientras el worker avanzó 3 ticks sin bloquearse.

### Q-6. ¿Para qué usamos el tag `EXT-X-DISCONTINUITY`? (explicación simple)

Cada segmento trae adentro sus propias marcas de tiempo (PTS): `segment0` dice "soy los segundos 1-11", `segment1` "soy los 11-21"... y `segment63` termina cerca del segundo 636. El player usa esas marcas para pegar los segmentos y mantener audio/video sincronizados, asumiendo que el tiempo siempre avanza.

Cuando el loop da la vuelta, después de `segment63` (~seg 636) servimos `segment0` (~seg 1): para el player el tiempo saltó **hacia atrás** 635 segundos, y sin aviso lo interpreta como datos corruptos (se congela, salta o corta el audio). `#EXT-X-DISCONTINUITY`, puesto en la playlist justo antes de `segment0`, le avisa: "lo que viene empieza una línea de tiempo nueva; resetea tu reloj interno y sigue". Analogía: el corte a comerciales en TV, nadie espera que el comercial continúe los timestamps del programa.

Es el único lugar donde hace falta porque los 64 segmentos entre sí son PTS-continuos (P-1); el salto ocurre solo en el cruce fin → inicio, una vez por vuelta (~10.5 min).

### Q-7. ¿Qué pasa con el segmento de 4.566667 s? ¿Afecta que `TARGETDURATION` sea 10?

**El tick es variable, no fijo.** El worker duerme hasta que el segmento al frente de la ventana termina de emitirse. Con `segment63` al frente (secuencia 63), el siguiente tick llega a los 4.566667 s en lugar de 10; la playlist declara su duración real en `#EXTINF:4.566667,` y el player lo reproduce completo sin notar nada. Si el tick fuera fijo de 10 s, cada vuelta publicaría 634.57 s de video en 640 s de reloj: 5.4 s de atraso por vuelta y corte por buffer vacío en ~1 hora (por eso D-9). Cubierto por `TestWindowAt/tick_corto_de_4s`.

**`TARGETDURATION` es un techo, no una promesa exacta**: "ningún segmento dura más de 10 s". Un segmento más corto es legal; la duración real va en su `#EXTINF`. El player usa el target principalmente como ritmo de repregunta de la playlist; si un cliente repregunta un poco tarde respecto del tick corto y pide un segmento recién salido, lo cubre el segmento de gracia (Q-4). Lo que sí rompería players es lo inverso (target menor que un segmento real); por eso se calcula como techo de la duración máxima y nunca cambia.

### Q-8. ¿Para qué sirve `EXT-X-DISCONTINUITY-SEQUENCE:0`? ¿Alguna vez cambia?

Es el contador de discontinuidades que **ya desaparecieron** de la playlist. Un player que se conecta tarde solo ve la ventana de 3; necesita saber cuántos saltos de línea de tiempo hubo antes para ubicar lo que ve dentro de la línea de tiempo global y no confundirse entre recargas consecutivas ([RFC 8216, sección 4.3.3.3](https://datatracker.ietf.org/doc/html/rfc8216#section-4.3.3.3)).

Sí cambia, una vez por vuelta:

| Momento | Estado del tag | Valor |
|---|---|---|
| Secuencias 0-63 | sin tag o visible | 0 |
| Secuencia 64 | tag al frente, aún visible | 0 |
| Secuencia 65 | el tag salió de la ventana | 1 |
| Secuencia 129 (segunda vuelta) | ídem | 2 |

En el código es `(k-1)/N` en `WindowAt`, verificado por los tests `sale_el_tag_de_discontinuidad` y `segunda_discontinuidad_removida`. Se emite siempre (incluso en 0) porque es válido según el RFC y más simple que agregarla condicionalmente. En el panel del player, el contador "Discontinuidades" sube en 1 un tick después de cada vuelta.
