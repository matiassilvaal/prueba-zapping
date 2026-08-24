package stream

import (
	"fmt"
	"os"
	"path/filepath"
)

// SegmentLoader obtiene los bytes de un segmento por nombre.
type SegmentLoader func(name string) ([]byte, error)

// Devuelve un loader que lee archivos desde un directorio
//
// @param [string] dir: directorio con los segmentos
//
// @return [SegmentLoader] loader basado en os.ReadFile
func DirLoader(dir string) SegmentLoader {
	return func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, name))
	}
}

// Comprueba que todos los segmentos del manifiesto existen en el directorio
//
// @param [string] dir: directorio con los segmentos
// @param [[]Segment] segments: segmentos declarados
//
// @return [error] primer archivo faltante o ilegible
func VerifyFiles(dir string, segments []Segment) error {
	for _, s := range segments {
		if _, err := os.Stat(filepath.Join(dir, s.Name)); err != nil {
			return fmt.Errorf("stream: segmento %q no disponible: %w", s.Name, err)
		}
	}
	return nil
}

// segmentSet es un conjunto inmutable de segmentos cargados en memoria.
type segmentSet struct {
	data map[string][]byte
}

// Construye un set con los nombres indicados, reutilizando los bytes ya
// presentes en prev y cargando solo los faltantes. Los required son
// obligatorios: si uno falla no se construye el set. Los optional se cargan
// best-effort: un fallo solo los deja fuera y se reporta en skipped
//
// @param [*segmentSet] prev: set anterior (puede ser nil)
// @param [[]string] required: nombres que el set debe contener sí o sí
// @param [[]string] optional: nombres deseables (prefetch)
// @param [SegmentLoader] load: origen de los bytes faltantes
//
// @return [*segmentSet] set nuevo
// @return [[]string] skipped: opcionales que no se pudieron cargar
// @return [error] si falla un required (no se publica set parcial)
func buildSegmentSet(prev *segmentSet, required, optional []string, load SegmentLoader) (*segmentSet, []string, error) {
	data := make(map[string][]byte, len(required)+len(optional))
	for _, name := range required {
		if err := loadInto(data, prev, name, load); err != nil {
			return nil, nil, fmt.Errorf("stream: cargar segmento %q: %w", name, err)
		}
	}
	var skipped []string
	for _, name := range optional {
		if err := loadInto(data, prev, name, load); err != nil {
			skipped = append(skipped, name)
		}
	}
	return &segmentSet{data: data}, skipped, nil
}

// Copia un segmento al mapa desde prev o desde el loader; omite duplicados
//
// @param [map[string][]byte] data: destino
// @param [*segmentSet] prev: set anterior (puede ser nil)
// @param [string] name: nombre de archivo
// @param [SegmentLoader] load: origen de los bytes
//
// @return [error] error del loader
func loadInto(data map[string][]byte, prev *segmentSet, name string, load SegmentLoader) error {
	if _, ok := data[name]; ok {
		return nil
	}
	if prev != nil {
		if b, ok := prev.data[name]; ok {
			data[name] = b
			return nil
		}
	}
	b, err := load(name)
	if err != nil {
		return err
	}
	data[name] = b
	return nil
}

// Busca un segmento en el set
//
// @param [string] name: nombre de archivo
//
// @return [[]byte] contenido
// @return [bool] false si no pertenece al set
func (s *segmentSet) get(name string) ([]byte, bool) {
	b, ok := s.data[name]
	return b, ok
}

// Tamaño total en bytes de los segmentos cacheados (para logs)
//
// @return [int] bytes
func (s *segmentSet) bytes() int {
	n := 0
	for _, b := range s.data {
		n += len(b)
	}
	return n
}
