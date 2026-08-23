// Package auth implementa registro, login y sesiones server-side. Define las
// interfaces de persistencia (UserStore, SessionStore) sin depender de ninguna
// base de datos concreta.
package auth

import "errors"

var (
	// ErrEmailTaken indica que ya existe una cuenta con ese email.
	ErrEmailTaken = errors.New("auth: ya existe una cuenta con ese email")
	// ErrNotFound indica que el usuario o la sesión no existen (o expiró).
	ErrNotFound = errors.New("auth: no encontrado")
	// ErrInvalidCredentials indica email o contraseña incorrectos.
	ErrInvalidCredentials = errors.New("auth: credenciales inválidas")
)
