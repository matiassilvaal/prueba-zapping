package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"prueba-zapping/internal/auth"
)

// UserStore implementa auth.UserStore sobre PostgreSQL.
type UserStore struct {
	pool *pgxpool.Pool
}

// Crea el store de usuarios
//
// @param [*pgxpool.Pool] pool: conexión
//
// @return [*UserStore] store
func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

// Inserta un usuario; el email debe ser único
//
// @param [context.Context] ctx: contexto
// @param [string] name: nombre
// @param [string] email: email normalizado
// @param [[]byte] passwordHash: hash bcrypt
//
// @return [auth.User] usuario con ID y CreatedAt
// @return [error] auth.ErrEmailTaken si el email existe, u otro error SQL
func (s *UserStore) Create(ctx context.Context, name, email string, passwordHash []byte) (auth.User, error) {
	u := auth.User{Name: name, Email: email, PasswordHash: passwordHash}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (name, email, password_hash) VALUES ($1, $2, $3) RETURNING id, created_at`,
		name, email, passwordHash).Scan(&u.ID, &u.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return auth.User{}, auth.ErrEmailTaken
	}
	if err != nil {
		return auth.User{}, err
	}
	return u, nil
}

// Busca un usuario por email. Compara sobre lower(email) para usar el índice
// funcional de la migración 0002 y encontrar también filas insertadas sin la
// normalización de la app
//
// @param [context.Context] ctx: contexto
// @param [string] email: email normalizado
//
// @return [auth.User] usuario
// @return [error] auth.ErrNotFound si no existe
func (s *UserStore) FindByEmail(ctx context.Context, email string) (auth.User, error) {
	var u auth.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, email, password_hash, created_at FROM users WHERE lower(email) = $1`, email).
		Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.User{}, err
	}
	return u, nil
}
