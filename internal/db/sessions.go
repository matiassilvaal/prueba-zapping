package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prueba-zapping/internal/auth"
)

// SessionStore implementa auth.SessionStore sobre PostgreSQL.
type SessionStore struct {
	pool *pgxpool.Pool
}

// Crea el store de sesiones
//
// @param [*pgxpool.Pool] pool: conexión
//
// @return [*SessionStore] store
func NewSessionStore(pool *pgxpool.Pool) *SessionStore { return &SessionStore{pool: pool} }

// Inserta una sesión
//
// @param [context.Context] ctx: contexto
// @param [auth.Session] sess: sesión con hash, usuario y expiración
//
// @return [error] error SQL
func (s *SessionStore) Create(ctx context.Context, sess auth.Session) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		sess.TokenHash, sess.UserID, sess.ExpiresAt)
	return err
}

// Obtiene una sesión vigente por hash
//
// @param [context.Context] ctx: contexto
// @param [[]byte] tokenHash: hash del token
//
// @return [auth.Session] sesión
// @return [error] auth.ErrNotFound si no existe o expiró
func (s *SessionStore) Get(ctx context.Context, tokenHash []byte) (auth.Session, error) {
	sess := auth.Session{TokenHash: tokenHash}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, expires_at FROM sessions WHERE token_hash = $1 AND expires_at > now()`, tokenHash).
		Scan(&sess.UserID, &sess.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.Session{}, err
	}
	return sess, nil
}

// Elimina una sesión
//
// @param [context.Context] ctx: contexto
// @param [[]byte] tokenHash: hash del token
//
// @return [error] error SQL
func (s *SessionStore) Delete(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// Elimina las sesiones expiradas respecto de now
//
// @param [context.Context] ctx: contexto
// @param [time.Time] now: instante de referencia
//
// @return [int64] filas eliminadas
// @return [error] error SQL
func (s *SessionStore) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
