// Package migrations embebe los archivos SQL versionados del esquema.
package migrations

import "embed"

// FS contiene los archivos NNNN_nombre.sql en orden de aplicación.
//
//go:embed *.sql
var FS embed.FS
