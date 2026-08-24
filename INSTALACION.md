# Instalación — Prueba Zapping

Requisitos: Docker 24+ con Docker Compose v2. No hace falta Go ni ninguna otra herramienta.

1. Cargar la imagen (incluye la aplicación y los segmentos de video):

   ```bash
   docker load -i prueba-zapping.tar
   ```

2. Levantar la aplicación y su base de datos, desde la carpeta que contiene `docker-compose.yml`:

   ```bash
   docker compose up -d
   ```

3. Esperar unos 15 segundos (los healthchecks se ponen verdes) y comprobar:

   ```bash
   docker compose ps                      # app y db deben decir "healthy"
   curl http://localhost:8080/healthz     # debe responder "ok"
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
- Los logs de la aplicación: `docker compose logs -f app`.
