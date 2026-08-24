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
	_, token, _ := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"})
	now = now.Add(2 * time.Minute)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sesión expirada: %v", err)
	}
	if n, _ := svc.DeleteExpired(ctx); n != 1 {
		t.Fatalf("DeleteExpired: %d", n)
	}
}

// failingSessionStore fuerza el fallo de Create para probar la rama de error
// de openSession.
type failingSessionStore struct {
	SessionStore
	err error
}

func (f failingSessionStore) Create(context.Context, Session) error { return f.err }

func TestService_ErrorAlAbrirSesionNoDevuelveUsuario(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("db caída")
	svc := NewService(NewMemoryUserStore(), failingSessionStore{NewMemorySessionStore(), boom}, time.Hour)

	u, token, err := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"})
	if !errors.Is(err, boom) || u.ID != 0 || u.Email != "" || token != "" {
		t.Fatalf("registro con error debía devolver cero valores: %+v %q %v", u, token, err)
	}

	// El usuario quedó creado en el store; el login también falla al abrir sesión.
	u, token, err = svc.Login(ctx, "ana@example.com", "secreto123")
	if !errors.Is(err, boom) || u.ID != 0 || u.Email != "" || token != "" {
		t.Fatalf("login con error debía devolver cero valores: %+v %q %v", u, token, err)
	}
}

func TestService_BcryptAcotado(t *testing.T) {
	ctx := context.Background()
	svc := newTestService()
	svc.bcryptSem = make(chan struct{}, 1)

	// Con el único cupo ocupado, login y registro esperan el semáforo y
	// abortan cuando el contexto vence, sin ejecutar bcrypt.
	svc.bcryptSem <- struct{}{}
	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, _, err := svc.Login(timeoutCtx, "nadie@example.com", "secreto123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("login con el semáforo lleno debía vencer por contexto: %v", err)
	}
	if _, _, err := svc.Register(timeoutCtx, RegistrationInput{"Ana", "ana@example.com", "secreto123"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("registro con el semáforo lleno debía vencer por contexto: %v", err)
	}

	// Liberado el cupo, el flujo completo funciona y devuelve el cupo al salir.
	<-svc.bcryptSem
	if _, _, err := svc.Register(ctx, RegistrationInput{"Ana", "ana@example.com", "secreto123"}); err != nil {
		t.Fatalf("registro tras liberar el semáforo: %v", err)
	}
	if _, _, err := svc.Login(ctx, "ana@example.com", "secreto123"); err != nil {
		t.Fatalf("login tras liberar el semáforo: %v", err)
	}
	if len(svc.bcryptSem) != 0 {
		t.Fatalf("el semáforo debía quedar libre, tiene %d cupos tomados", len(svc.bcryptSem))
	}
}

func TestService_HashDummyPrecalculado(t *testing.T) {
	svc := newTestService()
	if len(svc.dummyHash) == 0 {
		t.Fatal("NewService debía precalcular el hash dummy: si no, el primer login con email inexistente paga dos bcrypt")
	}
}
