package auth

import (
	"context"
	"log/slog"
	"time"
)

// Limpia sesiones expiradas hasta que se cancele el contexto: la caché en
// proceso cada cacheEvery y la persistencia cada dbEvery
//
// @param [context.Context] ctx: cancelación
// @param [time.Duration] dbEvery: período de limpieza en la persistencia
// @param [time.Duration] cacheEvery: período de limpieza de la caché
// @param [*slog.Logger] logger: logger
func (s *Service) RunJanitor(ctx context.Context, dbEvery, cacheEvery time.Duration, logger *slog.Logger) {
	dbTicker := time.NewTicker(dbEvery)
	cacheTicker := time.NewTicker(cacheEvery)
	defer dbTicker.Stop()
	defer cacheTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cacheTicker.C:
			s.SweepCache()
		case <-dbTicker.C:
			if n, err := s.DeleteExpired(ctx); err != nil {
				logger.Error("no se pudieron borrar sesiones expiradas", "error", err)
			} else if n > 0 {
				logger.Info("sesiones expiradas eliminadas", "count", n)
			}
		}
	}
}
