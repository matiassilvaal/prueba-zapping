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

// Construye un set con exactamente los nombres indicados, reutilizando los
// bytes ya presentes en prev y cargando solo los faltantes
//
// @param [*segmentSet] prev: set anterior (puede ser nil)
// @param [[]string] names: nombres que debe contener el set nuevo
// @param [SegmentLoader] load: origen de los bytes faltantes
//
// @return [*segmentSet] set nuevo
// @return [error] si alguna carga falla (no se publica set parcial)
func buildSegmentSet(prev *segmentSet, names []string, load SegmentLoader) (*segmentSet, error) {
	data := make(map[string][]byte, len(names))
	for _, name := range names {
		if _, ok := data[name]; ok {
			continue
		}
		if prev != nil {
			if b, ok := prev.data[name]; ok {
				data[name] = b
				continue
			}
		}
		b, err := load(name)
		if err != nil {
			return nil, fmt.Errorf("stream: cargar segmento %q: %w", name, err)
		}
		data[name] = b
	}
	return &segmentSet{data: data}, nil
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
