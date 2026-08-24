package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"prueba-zapping/internal/auth"
)

var (
	_ auth.UserStore    = (*UserStore)(nil)
	_ auth.SessionStore = (*SessionStore)(nil)
)

func TestUserStore(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserStore(pool)

	u, err := users.Create(ctx, "Ana", "ana@example.com", []byte("hash"))
	if err != nil || u.ID == 0 || u.CreatedAt.IsZero() {
		t.Fatalf("create: %v %+v", err, u)
	}
	if _, err := users.Create(ctx, "Otra", "ana@example.com", []byte("hash")); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("duplicado: %v", err)
	}
	got, err := users.FindByEmail(ctx, "ana@example.com")
	if err != nil || got.ID != u.ID || got.Name != "Ana" || string(got.PasswordHash) != "hash" {
		t.Fatalf("find: %v %+v", err, got)
	}
	if _, err := users.FindByEmail(ctx, "nadie@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
}

func TestSessionStore(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	u, _ := NewUserStore(pool).Create(ctx, "Ana", "ana@example.com", []byte("hash"))
	sessions := NewSessionStore(pool)

	live := auth.Session{TokenHash: auth.HashToken("viva"), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	dead := auth.Session{TokenHash: auth.HashToken("muerta"), UserID: u.ID, ExpiresAt: time.Now().Add(-time.Minute)}
	for _, s := range []auth.Session{live, dead} {
		if err := sessions.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := sessions.Get(ctx, live.TokenHash)
	if err != nil || got.UserID != u.ID || got.ExpiresAt.Sub(live.ExpiresAt).Abs() > time.Second {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := sessions.Get(ctx, dead.TokenHash); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expirada: %v", err)
	}
	if n, err := sessions.DeleteExpired(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("delete expired: %d %v", n, err)
	}
	if err := sessions.Delete(ctx, live.TokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Get(ctx, live.TokenHash); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("borrada: %v", err)
	}
}
