// Package web holds the admin UI, compiled into the binary.
package web

import "embed"

//go:embed admin
var Admin embed.FS
