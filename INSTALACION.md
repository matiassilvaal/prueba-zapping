# Instalación — Prueba Zapping

Requisitos: Docker 24+ con Docker Compose v2. No hace falta Go ni ninguna otra herramienta.

1. Cargar la imagen según la arquitectura de tu máquina (`uname -m`); ambas
   incluyen la aplicación y los segmentos de video y quedan con el mismo nombre
   (`prueba-zapping:latest`), así que el resto de los pasos no cambia:

   ```bash
   # x86_64 / amd64 (Intel o AMD)
   docker load -i prueba-zapping-amd64.tar

   # arm64 / aarch64 (Apple Silicon, ARM)
   docker load -i prueba-zapping-arm64.tar
   ```

2. Levantar la aplicación y su base de datos, desde la carpeta que contiene `docker-compose.yml`:

   ```bash
   docker compose up -d
   ```

3. Esperar unos 15 segundos (los healthchecks se ponen verdes) y comprobar:

   ```bash
   docker compose ps                      # app y db deben decir "healthy"
   curl http://localhost:8080/healthz     # debe responder "ok"
   # (si responde "not ready", la app aún está arrancando o la DB no está lista;
   #  el detalle queda en los logs: docker compose logs app)
   ```

4. Abrir <http://localhost:8080> en el navegador, crear una cuenta y entrar al player.

5. Detener:

   ```bash
   docker compose down       # conserva los usuarios registrados (volumen pgdata)
   docker compose down -v    # borra también la base de datos
   ```

Notas:

- La base de datos usa la imagen pública `postgres:17-alpine`; sus datos persisten en el volumen `pgdata`.
- Si el puerto 8080 está ocupado, cambiar el mapeo en `docker-compose.yml` (por ejemplo `"8081:8080"`).
- El apagado es ordenado: `docker compose down` termina en segundos aunque haya espectadores conectados (las conexiones SSE se cierran desde el servidor).
- Los logs de la aplicación: `docker compose logs -f app`.
