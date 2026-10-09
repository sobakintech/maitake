//go:build !embedui

package web

import "io/fs"

// Dist returns nil unless built with -tags embedui; in dev the Vite server on :3000 serves the UI.
func Dist() fs.FS { return nil }
