// Package config lee la configuración del servicio desde variables de entorno.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config agrupa todos los parámetros del servicio.
type Config struct {
	Port             int
	DatabaseURL      string
	DBMaxConns       int32
	SegmentsDir      string
	SegmentsManifest string
	SessionTTL       time.Duration
	CookieSecure     bool
	LogLevel         slog.Level
}

// Carga la configuración usando una función de búsqueda (inyectable en tests)
//
// @param [func(string) (string, bool)] lookup: equivalente a os.LookupEnv
//
// @return [Config] configuración validada con defaults aplicados
// @return [error] si falta una variable obligatoria o un valor es inválido
func Load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		Port:             8080,
		DBMaxConns:       10,
		SegmentsDir:      "/data/segments",
		SegmentsManifest: "segment.m3u8",
		SessionTTL:       24 * time.Hour,
		LogLevel:         slog.LevelInfo,
	}
	var ok bool
	if cfg.DatabaseURL, ok = lookup("DATABASE_URL"); !ok || cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("config: la variable DATABASE_URL es obligatoria")
	}
	if v, ok := lookup("PORT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 65535 {
			return cfg, fmt.Errorf("config: PORT inválido %q", v)
		}
		cfg.Port = n
	}
	if v, ok := lookup("DB_MAX_CONNS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("config: DB_MAX_CONNS inválido %q", v)
		}
		cfg.DBMaxConns = int32(n)
	}
	if v, ok := lookup("SEGMENTS_DIR"); ok && v != "" {
		cfg.SegmentsDir = v
	}
	if v, ok := lookup("SEGMENTS_MANIFEST"); ok && v != "" {
		cfg.SegmentsManifest = v
	}
	if v, ok := lookup("SESSION_TTL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return cfg, fmt.Errorf("config: SESSION_TTL inválido %q", v)
		}
		cfg.SessionTTL = d
	}
	if v, ok := lookup("COOKIE_SECURE"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("config: COOKIE_SECURE inválido %q", v)
		}
		cfg.CookieSecure = b
	}
	if v, ok := lookup("LOG_LEVEL"); ok {
		var lvl slog.Level
		if err := lvl.UnmarshalText([]byte(v)); err != nil {
			return cfg, fmt.Errorf("config: LOG_LEVEL inválido %q", v)
		}
		cfg.LogLevel = lvl
	}
	return cfg, nil
}

// Carga la configuración desde el entorno del proceso
//
// @return [Config] configuración validada
// @return [error] ver Load
func FromEnv() (Config, error) {
	return Load(os.LookupEnv)
}
