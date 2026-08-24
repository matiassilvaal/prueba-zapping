// Package db conecta con PostgreSQL, aplica migraciones e implementa los
// stores de auth sobre pgx.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Abre un pool de conexiones y verifica conectividad
//
// @param [context.Context] ctx: contexto
// @param [string] url: DSN de PostgreSQL
// @param [int32] maxConns: tamaño máximo del pool
//
// @return [*pgxpool.Pool] pool listo
// @return [error] si la configuración o el ping fallan
func Connect(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("db: DATABASE_URL inválida: %w", err)
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: crear pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: no se pudo conectar: %w", err)
	}
	return pool, nil
}
