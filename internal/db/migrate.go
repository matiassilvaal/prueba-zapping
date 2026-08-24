package db

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationLock es la clave del advisory lock que serializa migraciones entre réplicas.
const migrationLock = 8816_2026

// Aplica las migraciones pendientes de fsys en orden lexicográfico
//
// @param [context.Context] ctx: contexto
// @param [*pgxpool.Pool] pool: conexión
// @param [fs.FS] fsys: sistema de archivos con *.sql
//
// @return [int] migraciones aplicadas en esta corrida
// @return [error] error de lectura o SQL (la migración fallida se revierte)
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (int, error) {
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return 0, fmt.Errorf("db: listar migraciones: %w", err)
	}
	sort.Strings(files)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("db: adquirir conexión: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return 0, fmt.Errorf("db: advisory lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLock)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return 0, fmt.Errorf("db: crear schema_migrations: %w", err)
	}

	applied := 0
	for _, name := range files {
		version := strings.TrimSuffix(name, ".sql")
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists); err != nil {
			return applied, fmt.Errorf("db: consultar migración %s: %w", version, err)
		}
		if exists {
			continue
		}
		sqlBytes, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, fmt.Errorf("db: leer %s: %w", name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return applied, fmt.Errorf("db: iniciar transacción: %w", err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			tx.Rollback(ctx)
			return applied, fmt.Errorf("db: aplicar %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			tx.Rollback(ctx)
			return applied, fmt.Errorf("db: registrar %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("db: confirmar %s: %w", name, err)
		}
		applied++
	}
	return applied, nil
}
