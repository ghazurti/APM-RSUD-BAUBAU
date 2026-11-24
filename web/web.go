// Package web menyematkan template dan aset statis ke dalam binary (apm.exe berdiri sendiri, tanpa CDN).
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var files embed.FS

func Templates() fs.FS { sub, _ := fs.Sub(files, "templates"); return sub }
func Static() fs.FS    { sub, _ := fs.Sub(files, "static"); return sub }
