package auth

import (
	"context"
	"sync"
	"time"
)

// MemoryUserStore es un UserStore en memoria para tests.
type MemoryUserStore struct {
	mu     sync.Mutex
	byMail map[string]User
	nextID int64
}

// Crea un store de usuarios en memoria
//
// @return [*MemoryUserStore] store vacío
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{byMail: make(map[string]User)}
}

// Crea un usuario; falla con ErrEmailTaken si el email existe
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [string] name: nombre
// @param [string] email: email normalizado
// @param [[]byte] passwordHash: hash bcrypt
//
// @return [User] usuario creado con ID asignado
// @return [error] ErrEmailTaken
func (m *MemoryUserStore) Create(ctx context.Context, name, email string, passwordHash []byte) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byMail[email]; ok {
		return User{}, ErrEmailTaken
	}
	m.nextID++
	u := User{ID: m.nextID, Name: name, Email: email, PasswordHash: passwordHash, CreatedAt: time.Now()}
	m.byMail[email] = u
	return u, nil
}

// Busca un usuario por email
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [string] email: email normalizado
//
// @return [User] usuario
// @return [error] ErrNotFound
func (m *MemoryUserStore) FindByEmail(ctx context.Context, email string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byMail[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

// MemorySessionStore es un SessionStore en memoria para tests; cuenta las lecturas.
type MemorySessionStore struct {
	mu   sync.Mutex
	data map[string]Session
	gets int
}

// Crea un store de sesiones en memoria
//
// @return [*MemorySessionStore] store vacío
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{data: make(map[string]Session)}
}

// Guarda una sesión
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [Session] s: sesión
//
// @return [error] siempre nil
func (m *MemorySessionStore) Create(ctx context.Context, s Session) error {
	m.mu.Lock()
	m.data[string(s.TokenHash)] = s
	m.mu.Unlock()
	return nil
}

// Obtiene una sesión no expirada
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [[]byte] tokenHash: hash del token
//
// @return [Session] sesión
// @return [error] ErrNotFound si no existe o expiró
func (m *MemorySessionStore) Get(ctx context.Context, tokenHash []byte) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	s, ok := m.data[string(tokenHash)]
	if !ok || !time.Now().Before(s.ExpiresAt) {
		return Session{}, ErrNotFound
	}
	return s, nil
}

// Elimina una sesión
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [[]byte] tokenHash: hash del token
//
// @return [error] siempre nil
func (m *MemorySessionStore) Delete(ctx context.Context, tokenHash []byte) error {
	m.mu.Lock()
	delete(m.data, string(tokenHash))
	m.mu.Unlock()
	return nil
}

// Elimina las sesiones expiradas respecto de now
//
// @param [context.Context] ctx: contexto (ignorado)
// @param [time.Time] now: instante de referencia
//
// @return [int64] cantidad eliminada
// @return [error] siempre nil
func (m *MemorySessionStore) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k, s := range m.data {
		if !now.Before(s.ExpiresAt) {
			delete(m.data, k)
			n++
		}
	}
	return n, nil
}

// Cantidad de llamadas a Get (para verificar el uso de caché en tests)
//
// @return [int] lecturas acumuladas
func (m *MemorySessionStore) Gets() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gets
}
