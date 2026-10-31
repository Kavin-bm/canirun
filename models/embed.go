// Package models embeds the built-in model catalog (the *.yaml files in this
// directory) and community benchmark results (results/*.yaml) into the binary.
package models

import "embed"

// FS holds every catalog and result file.
//
//go:embed *.yaml all:results
var FS embed.FS
