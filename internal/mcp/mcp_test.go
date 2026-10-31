package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kavinbm16/canirun/internal/bench"
	"github.com/kavinbm16/canirun/internal/hw"
)

func session(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	s := &Server{
		Detect: func() hw.Info {
			return hw.Info{RAMBytes: 24 << 30, UnifiedMemory: true, GPU: "Apple M4 Pro", GPUBandwidthGBps: 273, CPUBandwidthGBps: 136}
		},
		Ollama:    func() *bench.Ollama { return &bench.Ollama{Base: "http://127.0.0.1:1", Client: http.DefaultClient} },
		Version:   "test",
		Overrides: func(h hw.Info, ram, vram, bw float64) (hw.Info, error) { return h, nil },
	}
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad line %q", l)
		}
		res = append(res, m)
	}
	return res
}

func TestHandshakeAndTools(t *testing.T) {
	res := session(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"estimate_model","arguments":{"model":"qwen3:8b"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"estimate_model","arguments":{"model":"nope"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"benchmark_model","arguments":{"model":"llama3.2"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"bogus"}`,
		`not json`,
	)
	if len(res) != 7 {
		t.Fatalf("want 7 responses (notification gets none), got %d", len(res))
	}
	init := res[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-03-26" {
		t.Errorf("version negotiation: %v", init["protocolVersion"])
	}
	if n := len(res[1]["result"].(map[string]any)["tools"].([]any)); n != 6 {
		t.Errorf("tools: %d", n)
	}
	est := res[2]["result"].(map[string]any)
	if est["isError"] == true || est["structuredContent"].(map[string]any)["verdict"] != "fits" {
		t.Errorf("estimate: %v", est)
	}
	if res[3]["result"].(map[string]any)["isError"] != true {
		t.Errorf("unknown model should be a tool error: %v", res[3])
	}
	if res[4]["result"].(map[string]any)["isError"] != true {
		t.Errorf("bench without Ollama should be a tool error: %v", res[4])
	}
	if res[5]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Errorf("unknown method: %v", res[5])
	}
	if res[6]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Errorf("parse error: %v", res[6])
	}
}
