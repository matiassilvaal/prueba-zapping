package auth

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Límites de validación del registro.
const (
	MaxNameLen     = 100
	MinPasswordLen = 8
	MaxPasswordLen = 72 // límite de bcrypt
)

// bcryptCost es el costo de hash; los tests lo reducen.
var bcryptCost = 12

// User es una cuenta registrada.
type User struct {
	ID           int64
	Name         string
	Email        string
	PasswordHash []byte
	CreatedAt    time.Time
}

// UserStore persiste usuarios.
type UserStore interface {
	Create(ctx context.Context, name, email string, passwordHash []byte) (User, error)
	FindByEmail(ctx context.Context, email string) (User, error)
}

// RegistrationInput son los datos del formulario de registro.
type RegistrationInput struct {
	Name     string
	Email    string
	Password string
}

// FieldError es un error de validación asociado a un campo del formulario.
type FieldError struct {
	Field   string
	Message string
}

// ValidationErrors agrupa errores de validación; implementa error.
type ValidationErrors []FieldError

// Describe todos los errores de validación en una línea
//
// @return [string] mensajes separados por "; "
func (v ValidationErrors) Error() string {
	parts := make([]string, len(v))
	for i, e := range v {
		parts[i] = e.Field + ": " + e.Message
	}
	return "auth: validación: " + strings.Join(parts, "; ")
}

// Indexa los errores por campo para mostrarlos en el formulario
//
// @return [map[string]string] campo → mensaje
func (v ValidationErrors) ByField() map[string]string {
	m := make(map[string]string, len(v))
	for _, e := range v {
		if _, ok := m[e.Field]; !ok {
			m[e.Field] = e.Message
		}
	}
	return m
}

// Valida y normaliza los datos de registro (recorta espacios, email en minúsculas)
//
// @param [RegistrationInput] in: datos crudos del formulario
//
// @return [RegistrationInput] datos normalizados
// @return [error] ValidationErrors si algún campo es inválido
func ValidateRegistration(in RegistrationInput) (RegistrationInput, error) {
	var errs ValidationErrors
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		errs = append(errs, FieldError{"name", "El nombre es obligatorio"})
	case n > MaxNameLen:
		errs = append(errs, FieldError{"name", fmt.Sprintf("El nombre no puede superar %d caracteres", MaxNameLen)})
	}
	if in.Email == "" {
		errs = append(errs, FieldError{"email", "El email es obligatorio"})
	} else if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
		errs = append(errs, FieldError{"email", "El email no es válido"})
	}
	switch n := len(in.Password); {
	case n < MinPasswordLen:
		errs = append(errs, FieldError{"password", fmt.Sprintf("La contraseña debe tener al menos %d caracteres", MinPasswordLen)})
	case n > MaxPasswordLen:
		errs = append(errs, FieldError{"password", fmt.Sprintf("La contraseña no puede superar %d caracteres", MaxPasswordLen)})
	}
	if len(errs) > 0 {
		return in, errs
	}
	return in, nil
}

// Genera el hash bcrypt de una contraseña
//
// @param [string] password: contraseña en claro
//
// @return [[]byte] hash
// @return [error] si bcrypt falla
func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
}

// Comprueba una contraseña contra su hash
//
// @param [[]byte] hash: hash almacenado
// @param [string] password: contraseña en claro
//
// @return [bool] true si coincide
func CheckPassword(hash []byte, password string) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}
