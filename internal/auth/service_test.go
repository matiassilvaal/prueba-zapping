package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestService() *Service {
	return NewService(NewMemoryUserStore(), NewMemorySessionStore(), time.Hour)
}

func TestService_RegistroLoginLogout(t *testing.T) {
	ctx := context.Background()
	svc := newTestService()

	u, token, err := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"})
	if err != nil || u.ID == 0 || token == "" {
		t.Fatalf("registro: %v %+v %q", err, u, token)
	}
	if id, err := svc.Authenticate(ctx, token); err != nil || id != u.ID {
		t.Fatalf("authenticate tras registro: %d %v", id, err)
	}

	_, _, err = svc.Register(ctx, RegistrationInput{"Otra", "ANA@example.com", "secreto123"})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("email duplicado: %v", err)
	}
	_, _, err = svc.Register(ctx, RegistrationInput{"", "x", "1"})
	var verr ValidationErrors
	if !errors.As(err, &verr) {
		t.Fatalf("validación: %v", err)
	}

	if _, _, err := svc.Login(ctx, "ana@example.com", "incorrecta"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("contraseña incorrecta: %v", err)
	}
	if _, _, err := svc.Login(ctx, "nadie@example.com", "secreto123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("usuario inexistente: %v", err)
	}
	u2, token2, err := svc.Login(ctx, " Ana@Example.com ", "secreto123")
	if err != nil || u2.ID != u.ID || token2 == "" || token2 == token {
		t.Fatalf("login: %v", err)
	}

	if err := svc.Logout(ctx, token2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sesión cerrada debía fallar: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "token-inventado"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token inválido: %v", err)
	}
}

func TestService_AuthenticateUsaCache(t *testing.T) {
	ctx := context.Background()
	sessions := NewMemorySessionStore()
	svc := NewService(NewMemoryUserStore(), sessions, time.Hour)
	_, token, err := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"})
	if err != nil {
		t.Fatal(err)
	}
	before := sessions.Gets()
	for i := 0; i < 5; i++ {
		if _, err := svc.Authenticate(ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	if sessions.Gets() != before {
		t.Fatalf("Authenticate consultó el store %d veces con caché caliente", sessions.Gets()-before)
	}
}

func TestService_SesionExpirada(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryUserStore(), NewMemorySessionStore(), time.Minute)
	now := time.Now()
	svc.now = func() time.Time { return now }
	svc.cache.now = svc.now
	_, token, _ := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"})
	now = now.Add(2 * time.Minute)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sesión expirada: %v", err)
	}
	if n, _ := svc.DeleteExpired(ctx); n != 1 {
		t.Fatalf("DeleteExpired: %d", n)
	}
}
