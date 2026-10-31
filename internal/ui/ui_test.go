package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kavinbm/canirun/internal/bench"
	"github.com/kavinbm/canirun/internal/hw"
)

func testServer() http.Handler {
	s := &Server{
		Detect: func() hw.Info {
			return hw.Info{RAMBytes: 24 << 30, UnifiedMemory: true, GPU: "Apple M4 Pro", GPUBandwidthGBps: 273, CPUBandwidthGBps: 136}
		},
		Ollama:  func() *bench.Ollama { return &bench.Ollama{Base: "http://127.0.0.1:1", Client: http.DefaultClient} },
		Version: "test",
	}
	return s.Handler()
}

func do(h http.Handler, method, target string, mod func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(`{"model":"llama3.2"}`))
	r.Host = "127.0.0.1:7878"
	if mod != nil {
		mod(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLocalOnlyGuard(t *testing.T) {
	h := testServer()
	cases := []struct {
		name string
		mod  func(*http.Request)
		meth string
		want int
	}{
		{"loopback ok", nil, "GET", 200},
		{"localhost ok", func(r *http.Request) { r.Host = "localhost:7878" }, "GET", 200},
		{"dns rebinding", func(r *http.Request) { r.Host = "evil.example:7878" }, "GET", 403},
		{"cross-site post", func(r *http.Request) {
			r.Header.Set("Origin", "http://evil.example")
			r.Header.Set("Content-Type", "application/json")
		}, "POST", 403},
		{"form post", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, "POST", 415},
	}
	for _, c := range cases {
		target := "/api/hw"
		if c.meth == "POST" {
			target = "/api/bench"
		}
		if got := do(h, c.meth, target, c.mod).Code; got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	if csp := do(h, "GET", "/", nil).Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("missing CSP: %q", csp)
	}
}

func TestEndpoints(t *testing.T) {
	h := testServer()
	w := do(h, "GET", "/api/estimate?model=qwen3:8b&ctx=8192", nil)
	var e struct {
		Verdict string `json:"verdict"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Verdict != "fits" {
		t.Fatalf("estimate: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/estimate?model=nope", nil); w.Code != 400 {
		t.Errorf("bad model: %d", w.Code)
	}
	if w := do(h, "GET", "/api/recommend?ctx=-5", nil); w.Code != 400 {
		t.Errorf("bad ctx: %d", w.Code)
	}
	if w := do(h, "GET", "/api/ollama", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Errorf("ollama down: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "canirun") {
		t.Errorf("index: %d", w.Code)
	}
	if w := do(h, "GET", "/app.js", nil); w.Code != 200 {
		t.Errorf("app.js: %d", w.Code)
	}
}
