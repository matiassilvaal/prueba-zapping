package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func init() { bcryptCost = bcrypt.MinCost }

func TestValidateRegistration_Normaliza(t *testing.T) {
	in, err := ValidateRegistration(RegistrationInput{Name: "  Matías ", Email: " Ana@Example.COM ", Password: "secreto123"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Name != "Matías" || in.Email != "ana@example.com" || in.Password != "secreto123" {
		t.Fatalf("normalización: %+v", in)
	}
}

func TestValidateRegistration_Errores(t *testing.T) {
	cases := []struct {
		name  string
		in    RegistrationInput
		field string
	}{
		{"nombre vacío", RegistrationInput{"", "a@b.co", "12345678"}, "name"},
		{"nombre largo", RegistrationInput{strings.Repeat("x", 101), "a@b.co", "12345678"}, "name"},
		{"email vacío", RegistrationInput{"A", "", "12345678"}, "email"},
		{"email sin arroba", RegistrationInput{"A", "ab.co", "12345678"}, "email"},
		{"email con nombre", RegistrationInput{"A", "Ana <a@b.co>", "12345678"}, "email"},
		{"contraseña corta", RegistrationInput{"A", "a@b.co", "1234567"}, "password"},
		{"contraseña larga", RegistrationInput{"A", "a@b.co", strings.Repeat("x", 73)}, "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateRegistration(tc.in)
			var verr ValidationErrors
			if !errors.As(err, &verr) {
				t.Fatalf("se esperaba ValidationErrors, got %v", err)
			}
			if _, ok := verr.ByField()[tc.field]; !ok {
				t.Fatalf("se esperaba error en %q, got %v", tc.field, verr)
			}
		})
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("secreto123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "secreto123") {
		t.Fatal("la contraseña correcta debía validar")
	}
	if CheckPassword(hash, "otra") {
		t.Fatal("una contraseña incorrecta no debía validar")
	}
}
