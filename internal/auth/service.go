package auth

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cacheTTL        = 30 * time.Second
	cacheMaxEntries = 10_000
)

// Service orquesta registro, login, logout y validación de sesiones.
type Service struct {
	users     UserStore
	sessions  SessionStore
	cache     *SessionCache
	ttl       time.Duration
	now       func() time.Time
	dummyHash []byte        // referencia para igualar el costo del login con email inexistente
	bcryptSem chan struct{} // acota los bcrypt concurrentes: sin tope, un bucle de POSTs satura la CPU
}

// Crea el servicio de autenticación. Precalcula el hash dummy (para que el
// primer login con email inexistente no pague dos bcrypt) y comparte su reloj
// con la caché de sesiones (los tests lo reemplazan en un solo lugar)
//
// @param [UserStore] users: persistencia de usuarios
// @param [SessionStore] sessions: persistencia de sesiones
// @param [time.Duration] ttl: duración de cada sesión
//
// @return [*Service] servicio
func NewService(users UserStore, sessions SessionStore, ttl time.Duration) *Service {
	s := &Service{
		users:     users,
		sessions:  sessions,
		cache:     NewSessionCache(cacheTTL, cacheMaxEntries),
		ttl:       ttl,
		now:       time.Now,
		bcryptSem: make(chan struct{}, runtime.GOMAXPROCS(0)),
	}
	s.dummyHash, _ = bcrypt.GenerateFromPassword([]byte("contraseña-de-relleno"), bcryptCost)
	s.cache.now = func() time.Time { return s.now() }
	return s
}

// Duración configurada de las sesiones
//
// @return [time.Duration] ttl
func (s *Service) TTL() time.Duration { return s.ttl }

// Registra un usuario y abre su sesión
//
// @param [context.Context] ctx: contexto
// @param [RegistrationInput] in: datos del formulario
//
// @return [User] usuario creado
// @return [string] token de sesión para la cookie
// @return [error] ValidationErrors, ErrEmailTaken u otro error del store
func (s *Service) Register(ctx context.Context, in RegistrationInput) (User, string, error) {
	in, err := ValidateRegistration(in)
	if err != nil {
		return User{}, "", err
	}
	hash, err := s.hashPassword(ctx, in.Password)
	if err != nil {
		return User{}, "", err
	}
	u, err := s.users.Create(ctx, in.Name, in.Email, hash)
	if err != nil {
		return User{}, "", err
	}
	token, err := s.openSession(ctx, u.ID)
	return u, token, err
}

// Valida credenciales y abre una sesión
//
// @param [context.Context] ctx: contexto
// @param [string] email: email (se normaliza)
// @param [string] password: contraseña en claro
//
// @return [User] usuario autenticado
// @return [string] token de sesión
// @return [error] ErrInvalidCredentials u otro error del store
func (s *Service) Login(ctx context.Context, email, password string) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.users.FindByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrNotFound):
		// Mismo costo que un login real.
		if _, err := s.checkPassword(ctx, s.dummyHash, password); err != nil {
			return User{}, "", err
		}
		return User{}, "", ErrInvalidCredentials
	case err != nil:
		return User{}, "", err
	}
	ok, err := s.checkPassword(ctx, u.PasswordHash, password)
	if err != nil {
		return User{}, "", err
	}
	if !ok {
		return User{}, "", ErrInvalidCredentials
	}
	token, err := s.openSession(ctx, u.ID)
	return u, token, err
}

// Cierra una sesión en la persistencia y en la caché
//
// @param [context.Context] ctx: contexto
// @param [string] token: token de la cookie
//
// @return [error] error del store
func (s *Service) Logout(ctx context.Context, token string) error {
	hash := HashToken(token)
	s.cache.Delete(hash)
	return s.sessions.Delete(ctx, hash)
}

// Resuelve el usuario de un token, usando la caché antes que la persistencia
//
// @param [context.Context] ctx: contexto
// @param [string] token: token de la cookie
//
// @return [int64] id de usuario
// @return [error] ErrNotFound si el token no corresponde a una sesión vigente
func (s *Service) Authenticate(ctx context.Context, token string) (int64, error) {
	if token == "" {
		return 0, ErrNotFound
	}
	hash := HashToken(token)
	if id, ok := s.cache.Get(hash); ok {
		return id, nil
	}
	sess, err := s.sessions.Get(ctx, hash)
	if err != nil {
		return 0, err
	}
	if !s.now().Before(sess.ExpiresAt) {
		return 0, ErrNotFound
	}
	s.cache.Put(hash, sess.UserID, sess.ExpiresAt)
	return sess.UserID, nil
}

// Elimina de la persistencia las sesiones expiradas
//
// @param [context.Context] ctx: contexto
//
// @return [int64] cantidad eliminada
// @return [error] error del store
func (s *Service) DeleteExpired(ctx context.Context) (int64, error) {
	return s.sessions.DeleteExpired(ctx, s.now())
}

// Elimina de la caché las entradas vencidas
//
// @return [int] cantidad eliminada
func (s *Service) SweepCache() int { return s.cache.Sweep() }

// Reserva un cupo de bcrypt o aborta si el contexto termina antes. bcrypt
// (costo 12) tarda cientos de milisegundos: sin tope, cada POST inválido a
// /login o /register consume una CPU y un bucle trivial satura el proceso
//
// @param [context.Context] ctx: cancelación de la espera
//
// @return [func()] libera el cupo (nil si err no es nil)
// @return [error] ctx.Err() si el contexto terminó antes de obtener cupo
func (s *Service) acquireBcrypt(ctx context.Context) (func(), error) {
	select {
	case s.bcryptSem <- struct{}{}:
		return func() { <-s.bcryptSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Genera el hash bcrypt de una contraseña respetando el cupo de concurrencia
//
// @param [context.Context] ctx: cancelación
// @param [string] password: contraseña en claro
//
// @return [[]byte] hash
// @return [error] ctx.Err() sin cupo, o error de bcrypt
func (s *Service) hashPassword(ctx context.Context, password string) ([]byte, error) {
	release, err := s.acquireBcrypt(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("auth: hash de contraseña: %w", err)
	}
	return hash, nil
}

// Comprueba una contraseña contra su hash respetando el cupo de concurrencia
//
// @param [context.Context] ctx: cancelación
// @param [[]byte] hash: hash almacenado
// @param [string] password: contraseña en claro
//
// @return [bool] true si coincide
// @return [error] ctx.Err() si no se obtuvo cupo
func (s *Service) checkPassword(ctx context.Context, hash []byte, password string) (bool, error) {
	release, err := s.acquireBcrypt(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return CheckPassword(hash, password), nil
}

// Crea y persiste una sesión nueva para un usuario
//
// @param [context.Context] ctx: contexto
// @param [int64] userID: usuario
//
// @return [string] token
// @return [error] error del generador o del store
func (s *Service) openSession(ctx context.Context, userID int64) (string, error) {
	token, err := NewToken()
	if err != nil {
		return "", fmt.Errorf("auth: generar token: %w", err)
	}
	sess := Session{TokenHash: HashToken(token), UserID: userID, ExpiresAt: s.now().Add(s.ttl)}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return "", err
	}
	s.cache.Put(sess.TokenHash, userID, sess.ExpiresAt)
	return token, nil
}
