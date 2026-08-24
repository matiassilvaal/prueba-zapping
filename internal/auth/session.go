package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// Session es una sesión server-side; solo se guarda el hash del token.
type Session struct {
	TokenHash []byte
	UserID    int64
	ExpiresAt time.Time
}

// SessionStore persiste sesiones. Get no devuelve sesiones expiradas.
type SessionStore interface {
	Create(ctx context.Context, s Session) error
	Get(ctx context.Context, tokenHash []byte) (Session, error)
	Delete(ctx context.Context, tokenHash []byte) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// Genera un token de sesión aleatorio (32 bytes, base64url sin padding)
//
// @return [string] token para la cookie
// @return [error] si el generador aleatorio falla
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Deriva el hash SHA-256 de un token; es lo único que se persiste
//
// @param [string] token: token de la cookie
//
// @return [[]byte] 32 bytes
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

type cacheEntry struct {
	userID    int64
	expiresAt time.Time
	cachedAt  time.Time
}

// SessionCache evita consultar la base de datos en cada request del stream.
// Acotada por TTL y por cantidad de entradas.
type SessionCache struct {
	mu         sync.RWMutex
	entries    map[string]cacheEntry
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
}

// Crea una caché de sesiones
//
// @param [time.Duration] ttl: tiempo máximo que una entrada se considera válida sin reconsultar la DB
// @param [int] maxEntries: tope de entradas; al alcanzarlo, Put no cachea nuevas
//
// @return [*SessionCache] caché vacía
func NewSessionCache(ttl time.Duration, maxEntries int) *SessionCache {
	return &SessionCache{entries: make(map[string]cacheEntry), ttl: ttl, maxEntries: maxEntries, now: time.Now}
}

// Busca una sesión vigente en caché
//
// @param [[]byte] hash: hash del token
//
// @return [int64] id de usuario
// @return [bool] false si no está, venció el TTL de caché o expiró la sesión
func (c *SessionCache) Get(hash []byte) (int64, bool) {
	c.mu.RLock()
	e, ok := c.entries[string(hash)]
	c.mu.RUnlock()
	if !ok {
		return 0, false
	}
	now := c.now()
	if now.Sub(e.cachedAt) > c.ttl || !now.Before(e.expiresAt) {
		c.Delete(hash)
		return 0, false
	}
	return e.userID, true
}

// Guarda o refresca una sesión en caché
//
// @param [[]byte] hash: hash del token
// @param [int64] userID: usuario dueño de la sesión
// @param [time.Time] expiresAt: expiración de la sesión
func (c *SessionCache) Put(hash []byte, userID int64, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := string(hash)
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		// Antes de rechazar la entrada nueva se barren las vencidas: si no,
		// una caché llena de sesiones muertas dejaría sin caché a los logins
		// nuevos hasta el próximo Sweep periódico.
		if c.sweepLocked() == 0 {
			return
		}
	}
	c.entries[key] = cacheEntry{userID: userID, expiresAt: expiresAt, cachedAt: c.now()}
}

// Elimina una sesión de la caché (logout)
//
// @param [[]byte] hash: hash del token
func (c *SessionCache) Delete(hash []byte) {
	c.mu.Lock()
	delete(c.entries, string(hash))
	c.mu.Unlock()
}

// Elimina las entradas vencidas
//
// @return [int] cantidad eliminada
func (c *SessionCache) Sweep() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sweepLocked()
}

// Elimina las entradas vencidas. Requiere c.mu tomado
//
// @return [int] cantidad eliminada
func (c *SessionCache) sweepLocked() int {
	now := c.now()
	n := 0
	for k, e := range c.entries {
		if now.Sub(e.cachedAt) > c.ttl || !now.Before(e.expiresAt) {
			delete(c.entries, k)
			n++
		}
	}
	return n
}

// Cantidad de entradas en caché
//
// @return [int] entradas
func (c *SessionCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
