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
