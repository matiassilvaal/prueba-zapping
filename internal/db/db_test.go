package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"prueba-zapping/migrations"
)

// testPool conecta a TEST_DATABASE_URL, migra y deja las tablas vacías.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL no definida; se omite el test de integración")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := Connect(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE sessions, users RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestMigrate_EsIdempotente(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	n, err := Migrate(ctx, pool, migrations.FS)
	if err != nil || n != 0 {
		t.Fatalf("segunda corrida: n=%d err=%v", n, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count < 1 {
		t.Fatalf("schema_migrations: %d %v", count, err)
	}
}
