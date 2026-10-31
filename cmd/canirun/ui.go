package main

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/kavinbm16/canirun/internal/ui"
)

func runUI(args []string, w io.Writer) error {
	fs := newFlags("ui")
	port := fs.Int("port", 7878, "port on 127.0.0.1 (falls back to a free port if taken)")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	if _, err := parse(fs, args, 0); err != nil {
		return err
	}
	if *port < 0 || *port > 65535 {
		return usageErr("--port must be 0-65535")
	}
	ln, err := ui.Listen(*port)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Fprintf(w, "canirun UI on %s (local only, Ctrl+C to stop)\n", url)
	if !*noOpen {
		openBrowser(url)
	}
	srv := &http.Server{
		Handler:           (&ui.Server{Detect: detect, Ollama: newOllama, Version: version}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.Serve(ln)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start() // best effort; the URL is printed anyway
}
