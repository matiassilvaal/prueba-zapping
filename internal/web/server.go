package web

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"prueba-zapping/internal/auth"
)

// Deps son las dependencias del sitio, inyectadas desde cmd/server.
type Deps struct {
	Auth         *auth.Service
	Stream       http.Handler // handler del stream sin autenticación; se protege aquí
	Ready        func(ctx context.Context) error
	SessionTTL   time.Duration
	CookieSecure bool
	Logger       *slog.Logger
}

// Server agrupa rutas y handlers de las páginas.
type Server struct {
	deps Deps
	r    *renderer
	mux  *http.ServeMux
}

// Construye el servidor web y registra sus rutas
//
// @param [Deps] d: dependencias
//
// @return [*Server] servidor
// @return [error] si las plantillas no compilan
func New(d Deps) (*Server, error) {
	r, err := newRenderer()
	if err != nil {
		return nil, err
	}
	s := &Server{deps: d, r: r, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// Handler HTTP con todas las rutas del sitio
//
// @return [http.Handler] mux
func (s *Server) Handler() http.Handler { return s.mux }

// Registra las rutas en el mux
func (s *Server) routes() {
	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", cacheControl("public, max-age=86400", http.StripPrefix("/static/", http.FileServerFS(static))))
	s.mux.HandleFunc("GET /{$}", s.root)
	s.mux.HandleFunc("GET /register", s.registerForm)
	s.mux.HandleFunc("POST /register", s.registerSubmit)
	s.mux.HandleFunc("GET /login", s.loginForm)
	s.mux.HandleFunc("POST /login", s.loginSubmit)
	s.mux.HandleFunc("POST /logout", s.logout)
	s.mux.Handle("GET /player", auth.RequireSession(s.deps.Auth, auth.RedirectToLogin, http.HandlerFunc(s.player)))
	s.mux.Handle("/stream/", http.StripPrefix("/stream", auth.RequireSession(s.deps.Auth, auth.Unauthorized, s.deps.Stream)))
}

// Envuelve un handler fijando el header Cache-Control
//
// @param [string] value: valor del header
// @param [http.Handler] next: handler envuelto
//
// @return [http.Handler] handler con el header
func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}

// Indica si el request trae una sesión válida
//
// @param [*http.Request] r: request
//
// @return [bool] true si la cookie corresponde a una sesión vigente
func (s *Server) isLoggedIn(r *http.Request) bool {
	_, err := s.deps.Auth.Authenticate(r.Context(), auth.TokenFromRequest(r))
	return err == nil
}

// Renderiza una vista y registra el error si falla
//
// @param [http.ResponseWriter] w: respuesta
// @param [int] status: código HTTP
// @param [string] page: página
// @param [pageData] data: modelo
func (s *Server) render(w http.ResponseWriter, status int, page string, data pageData) {
	if err := s.r.render(w, status, page, data); err != nil {
		s.deps.Logger.Error("no se pudo renderizar la vista", "page", page, "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
	}
}

// Muestra el formulario de registro (o redirige al player si ya hay sesión)
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) registerForm(w http.ResponseWriter, r *http.Request) {
	if s.isLoggedIn(r) {
		http.Redirect(w, r, "/player", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "register.html", newPageData("Crear cuenta"))
}

// Procesa el registro: crea la cuenta, abre sesión y redirige al player
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request con el formulario
func (s *Server) registerSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formulario inválido", http.StatusBadRequest)
		return
	}
	in := auth.RegistrationInput{
		Name:     r.PostFormValue("name"),
		Email:    r.PostFormValue("email"),
		Password: r.PostFormValue("password"),
	}
	_, token, err := s.deps.Auth.Register(r.Context(), in)
	if err != nil {
		data := newPageData("Crear cuenta")
		data.Form["name"] = in.Name
		data.Form["email"] = in.Email
		var verr auth.ValidationErrors
		switch {
		case errors.As(err, &verr):
			data.Errors = verr.ByField()
			s.render(w, http.StatusUnprocessableEntity, "register.html", data)
		case errors.Is(err, auth.ErrEmailTaken):
			data.Errors["email"] = "Ya existe una cuenta con ese email"
			s.render(w, http.StatusConflict, "register.html", data)
		default:
			s.deps.Logger.Error("registro falló", "error", err)
			data.Error = "No pudimos crear tu cuenta. Inténtalo de nuevo."
			s.render(w, http.StatusInternalServerError, "register.html", data)
		}
		return
	}
	auth.SetSessionCookie(w, token, s.deps.SessionTTL, s.deps.CookieSecure)
	http.Redirect(w, r, "/player", http.StatusSeeOther)
}

// Redirige la raíz al player o al login según haya sesión
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	if s.isLoggedIn(r) {
		http.Redirect(w, r, "/player", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Muestra el formulario de login (o redirige al player si ya hay sesión)
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if s.isLoggedIn(r) {
		http.Redirect(w, r, "/player", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "login.html", newPageData("Iniciar sesión"))
}

// Procesa el login: valida credenciales, abre sesión y redirige al player
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request con el formulario
func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formulario inválido", http.StatusBadRequest)
		return
	}
	email, password := r.PostFormValue("email"), r.PostFormValue("password")
	_, token, err := s.deps.Auth.Login(r.Context(), email, password)
	if err != nil {
		data := newPageData("Iniciar sesión")
		data.Form["email"] = email
		if errors.Is(err, auth.ErrInvalidCredentials) {
			data.Error = "Email o contraseña incorrectos"
			s.render(w, http.StatusUnauthorized, "login.html", data)
			return
		}
		s.deps.Logger.Error("login falló", "error", err)
		data.Error = "No pudimos iniciar sesión. Inténtalo de nuevo."
		s.render(w, http.StatusInternalServerError, "login.html", data)
		return
	}
	auth.SetSessionCookie(w, token, s.deps.SessionTTL, s.deps.CookieSecure)
	http.Redirect(w, r, "/player", http.StatusSeeOther)
}

// Muestra la página del player
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request autenticado
func (s *Server) player(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "player.html", newPageData("Player"))
}

// Cierra la sesión actual, borra la cookie y redirige al login
//
// @param [http.ResponseWriter] w: respuesta
// @param [*http.Request] r: request
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if token := auth.TokenFromRequest(r); token != "" {
		if err := s.deps.Auth.Logout(r.Context(), token); err != nil {
			s.deps.Logger.Error("logout falló", "error", err)
		}
	}
	auth.ClearSessionCookie(w, s.deps.CookieSecure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
