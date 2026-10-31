package main

import (
	"fmt"
	"io"
)

func runMCP(_ []string, _ io.Reader, w io.Writer) error {
	fmt.Fprintln(w, "mcp: coming soon. For now, agents can call `canirun <model> --json`.")
	return nil
}

func runUI(_ []string, w io.Writer) error {
	fmt.Fprintln(w, "ui: coming soon.")
	return nil
}
