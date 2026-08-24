package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cacheTTL        = 30 * time.Second
	cacheMaxEntries = 10_000
)

// Service orquesta registro, login, logout y validación de sesiones.
type Service struct {
	users    UserStore
	sessions SessionStore
	cache    *SessionCache
	ttl      time.Duration
	now      func() time.Time
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

// Hash de referencia para igualar el tiempo de respuesta cuando el email no existe
//
// @return [[]byte] hash bcrypt de una contraseña fija
func getDummyHash() []byte {
	dummyHashOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("contraseña-de-relleno"), bcryptCost)
	})
	return dummyHash
}

// Crea el servicio de autenticación
//
// @param [UserStore] users: persistencia de usuarios
// @param [SessionStore] sessions: persistencia de sesiones
// @param [time.Duration] ttl: duración de cada sesión
//
// @return [*Service] servicio
func NewService(users UserStore, sessions SessionStore, ttl time.Duration) *Service {
	return &Service{
		users:    users,
		sessions: sessions,
		cache:    NewSessionCache(cacheTTL, cacheMaxEntries),
		ttl:      ttl,
		now:      time.Now,
	}
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
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, "", fmt.Errorf("auth: hash de contraseña: %w", err)
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
		CheckPassword(getDummyHash(), password) // mismo costo que un login real
		return User{}, "", ErrInvalidCredentials
	case err != nil:
		return User{}, "", err
	}
	if !CheckPassword(u.PasswordHash, password) {
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
