package main

import (
	"io"
	"sync"

	"github.com/kavinbm16/canirun/internal/mcp"
)

// runMCP serves the Model Context Protocol on stdin/stdout.
func runMCP(args []string, r io.Reader, w io.Writer) error {
	fs := newFlags("mcp")
	if _, err := parse(fs, args, 0); err != nil {
		return err
	}
	cached := sync.OnceValue(detect) // hardware does not change during a session
	s := &mcp.Server{Detect: cached, Ollama: newOllama, Version: version, Overrides: applyOverrides}
	return s.Serve(r, w)
}
