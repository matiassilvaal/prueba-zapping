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
  CMD wget -qO- http://localhost:${PORT}/healthz || exit 1
ENTRYPOINT ["server"]
