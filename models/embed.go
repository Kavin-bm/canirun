// Package models embeds the built-in model catalog (the *.yaml files in this
// directory) into the binary.
package models

import "embed"

// FS holds every catalog file.
//
//go:embed *.yaml
var FS embed.FS
