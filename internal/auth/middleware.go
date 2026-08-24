package auth

import (
	"context"
	"net/http"
	"time"
)

// CookieName es el nombre de la cookie de sesión.
const CookieName = "session"

// Authenticator resuelve un token de sesión a un id de usuario.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (int64, error)
}

// FailureMode define qué hacer cuando no hay sesión válida.
type FailureMode int

const (
	// RedirectToLogin responde 303 a /login (páginas).
	RedirectToLogin FailureMode = iota
	// Unauthorized responde 401 JSON (stream, SSE).
	Unauthorized
)

type ctxKey struct{}

// Escribe la cookie de sesión
//
// @param [http.ResponseWriter] w: respuesta
// @param [string] token: token de sesión
// @param [time.Duration] ttl: duración de la cookie
// @param [bool] secure: flag Secure
func SetSessionCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Borra la cookie de sesión en el navegador
//
// @param [http.ResponseWriter] w: respuesta
// @param [bool] secure: flag Secure (debe coincidir con la cookie original)
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Extrae el token de sesión del request
//
// @param [*http.Request] r: request
//
// @return [string] token o cadena vacía si no hay cookie
func TokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// Middleware que exige sesión válida e inyecta el id de usuario en el contexto
//
// @param [Authenticator] a: validador de tokens
// @param [FailureMode] mode: redirección o 401 ante fallo
// @param [http.Handler] next: handler protegido
//
// @return [http.Handler] handler envuelto
func RequireSession(a Authenticator, mode FailureMode, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := a.Authenticate(r.Context(), TokenFromRequest(r))
		if err != nil {
			if mode == Unauthorized {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"Debes iniciar sesión para ver el stream"}`))
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

// Recupera el id de usuario inyectado por RequireSession
//
// @param [context.Context] ctx: contexto del request
//
// @return [int64] id de usuario
// @return [bool] false si no hay sesión en el contexto
func UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(ctxKey{}).(int64)
	return id, ok
}
