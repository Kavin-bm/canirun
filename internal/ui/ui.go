// Package ui serves the local web UI. It listens on 127.0.0.1 only, makes no
// external network calls, and all assets are embedded in the binary.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Kavin-bm/canirun/internal/bench"
	"github.com/Kavin-bm/canirun/internal/estimate"
	"github.com/Kavin-bm/canirun/internal/hw"
	"github.com/Kavin-bm/canirun/internal/model"
	"github.com/Kavin-bm/canirun/internal/report"
)

//go:embed static
var static embed.FS

// Server holds what the handlers need. Detect and Ollama are injectable for tests.
type Server struct {
	Detect  func() hw.Info
	Ollama  func() *bench.Ollama
	Version string

	once    sync.Once
	hwInfo  hw.Info
	benchMu sync.Mutex // one benchmark at a time: they compete for the GPU
}

func (s *Server) hw() hw.Info {
	s.once.Do(func() { s.hwInfo = s.Detect() })
	return s.hwInfo
}

// Handler returns the full HTTP handler, wrapped in the local-only guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(static, "static")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /api/hw", s.handleHW)
	mux.HandleFunc("GET /api/models", s.handleModels)
	mux.HandleFunc("GET /api/estimate", s.handleEstimate)
	mux.HandleFunc("GET /api/recommend", s.handleRecommend)
	mux.HandleFunc("GET /api/ollama", s.handleOllama)
	mux.HandleFunc("POST /api/bench", s.handleBench)
	return localOnly(mux)
}

// localOnly blocks DNS-rebinding (Host must be loopback) and cross-site
// requests (Origin, when sent, must be this server), and sets a strict CSP.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "canirun UI only answers on 127.0.0.1 / localhost", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "JSON only", http.StatusUnsupportedMediaType)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func ctxParam(r *http.Request) (int, error) {
	v := r.URL.Query().Get("ctx")
	if v == "" {
		return 8192, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || n > 10_000_000 {
		return 0, errors.New("ctx must be a positive number of tokens")
	}
	return n, nil
}

func (s *Server) handleHW(w http.ResponseWriter, _ *http.Request) {
	h := s.hw()
	writeJSON(w, http.StatusOK, map[string]any{"hardware": h, "budget": estimate.BudgetFor(h), "version": s.Version})
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	cat, err := model.Catalog()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cat)
}

func (s *Server) handleEstimate(w http.ResponseWriter, r *http.Request) {
	ctx, err := ctxParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	m, err := model.Resolve(r.URL.Query().Get("model"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	e, err := report.BuildEstimate(m, s.hw(), ctx, "")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleRecommend(w http.ResponseWriter, r *http.Request) {
	ctx, err := ctxParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	recs, err := report.Recommend(s.hw(), ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

func (s *Server) handleOllama(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	list, err := s.Ollama().List(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "error": err.Error(), "models": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "models": list})
}

func (s *Server) handleBench(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
		Ctx   int    `json:"ctx"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Model == "" {
		writeErr(w, http.StatusBadRequest, errors.New(`send {"model": "<ollama model>"}`))
		return
	}
	if req.Ctx <= 0 {
		req.Ctx = 8192
	}
	if !s.benchMu.TryLock() {
		writeErr(w, http.StatusConflict, errors.New("a benchmark is already running"))
		return
	}
	defer s.benchMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	res, err := s.Ollama().Bench(ctx, req.Model, 3)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, bench.ErrNotInstalled) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, report.Compare(res, "", s.hw(), req.Ctx))
}

// Listen binds 127.0.0.1:port, falling back to a free port if it is taken.
func Listen(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil && port != 0 {
		return net.Listen("tcp", "127.0.0.1:0")
	}
	return ln, err
}
