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
	// Con timeout: pg_advisory_lock espera indefinidamente si otra réplica
	// quedó colgada con el lock tomado; el arranque no debe hacerlo.
	migCtx, cancelMig := context.WithTimeout(ctx, 30*time.Second)
	applied, err := db.Migrate(migCtx, pool, migrations.FS)
	cancelMig()
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
			if err := streamSvc.Ready(); err != nil {
				return err
			}
			return pool.Ping(ctx)
		},
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

	// Goroutines de fondo. El hub se suscribe antes de arrancar el worker para
	// que reciba la primera ventana por contrato y no por el timing del primer
	// tick (hoy tarda porque lee de disco, pero eso es un accidente).
	errCh := make(chan error, 2)
	events, unsubscribe := streamSvc.Subscribe()
	defer unsubscribe()
	go hub.Run(ctx, events)
	go func() { errCh <- streamSvc.Run(ctx) }()
	go authSvc.RunJanitor(ctx, time.Hour, time.Minute, logger)
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
