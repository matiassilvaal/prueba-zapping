package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 || cfg.DBMaxConns != 10 || cfg.SegmentsDir != "/data/segments" ||
		cfg.SegmentsManifest != "segment.m3u8" || cfg.SessionTTL != 24*time.Hour ||
		cfg.CookieSecure || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("defaults inesperados: %+v", cfg)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		"DATABASE_URL": "postgres://x", "PORT": "9000", "DB_MAX_CONNS": "25",
		"SEGMENTS_DIR": "./segments", "SEGMENTS_MANIFEST": "m.m3u8",
		"SESSION_TTL": "2h", "COOKIE_SECURE": "true", "LOG_LEVEL": "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9000 || cfg.DBMaxConns != 25 || cfg.SegmentsDir != "./segments" ||
		cfg.SegmentsManifest != "m.m3u8" || cfg.SessionTTL != 2*time.Hour || !cfg.CookieSecure ||
		cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("overrides no aplicados: %+v", cfg)
	}
}

func TestLoad_Errores(t *testing.T) {
	cases := map[string]map[string]string{
		"falta DATABASE_URL": {},
		"puerto inválido":    {"DATABASE_URL": "x", "PORT": "abc"},
		"ttl inválido":       {"DATABASE_URL": "x", "SESSION_TTL": "mañana"},
		"nivel inválido":     {"DATABASE_URL": "x", "LOG_LEVEL": "loud"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(lookupFrom(env)); err == nil {
				t.Fatal("se esperaba error")
			} else if !strings.Contains(err.Error(), "config:") {
				t.Fatalf("mensaje sin prefijo: %v", err)
			}
		})
	}
}
