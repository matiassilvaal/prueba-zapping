// Package web sirve las páginas del sitio (registro, login, player), los
// assets estáticos y el canal de eventos SSE. Usa los paquetes auth y stream
// solo a través de sus interfaces públicas.
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"path"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// pageData es el modelo común de todas las vistas.
type pageData struct {
	Title  string
	Assets string // versión de los assets estáticos, para URLs con caché larga
	Form   map[string]string
	Errors map[string]string
	Error  string
}

// Crea el modelo de una vista con los mapas inicializados
//
// @param [string] title: título de la página
//
// @return [pageData] modelo listo para renderizar
func newPageData(title string) pageData {
	return pageData{Title: title, Assets: assetsVersion, Form: map[string]string{}, Errors: map[string]string{}}
}

// renderer mantiene una plantilla compilada por página, cada una con el layout.
type renderer struct {
	pages map[string]*template.Template
}

// Compila layout + cada página embebida
//
// @return [*renderer] renderer listo
// @return [error] si alguna plantilla no compila
func newRenderer() (*renderer, error) {
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	r := &renderer{pages: make(map[string]*template.Template)}
	for _, full := range names {
		name := path.Base(full)
		if name == "layout.html" {
			continue
		}
		t, err := template.ParseFS(templateFS, "templates/layout.html", full)
		if err != nil {
			return nil, fmt.Errorf("web: compilar %s: %w", name, err)
		}
		r.pages[name] = t
	}
	return r, nil
}

// Renderiza una página completa en un buffer y la escribe con el status dado
//
// @param [http.ResponseWriter] w: respuesta
// @param [int] status: código HTTP
// @param [string] page: nombre de archivo de la página (ej. "login.html")
// @param [any] data: modelo de la vista
//
// @return [error] si la página no existe o falla la ejecución (no se escribió nada)
func (r *renderer) render(w http.ResponseWriter, status int, page string, data any) error {
	t, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("web: página %q no registrada", page)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		return fmt.Errorf("web: renderizar %s: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// Versión de los assets embebidos: hash del contenido de static/. Cambia en
// cada build que toque un asset, de modo que las URLs ?v=<hash> invalidan la
// caché del navegador tras un despliegue (la caché puede ser larga e immutable)
//
// @return [string] primeros 12 hex del SHA-256 acumulado
func computeAssetsVersion() string {
	h := sha256.New()
	fs.WalkDir(staticFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := staticFS.ReadFile(p)
		if err != nil {
			return err
		}
		io.WriteString(h, p)
		h.Write(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

var assetsVersion = computeAssetsVersion()
