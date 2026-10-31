// Package mcp is a minimal Model Context Protocol server over stdio
// (newline-delimited JSON-RPC 2.0). It exposes the same functions as the CLI
// so coding agents (Claude Code, Codex, Gemini CLI) can call them.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Kavin-bm/canirun/internal/bench"
	"github.com/Kavin-bm/canirun/internal/hw"
	"github.com/Kavin-bm/canirun/internal/model"
	"github.com/Kavin-bm/canirun/internal/report"
)

// Server answers MCP requests. Detect and Ollama are injectable for tests.
type Server struct {
	Detect    func() hw.Info
	Ollama    func() *bench.Ollama
	Version   string
	Overrides func(h hw.Info, ramGB, vramGB, bwGBps float64) (hw.Info, error)
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

var supportedVersions = map[string]bool{"2024-11-05": true, "2025-03-26": true, "2025-06-18": true}

const latestVersion = "2025-06-18"

// Serve reads requests from r and writes responses to w until EOF.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 { // notification: no reply
			continue
		}
		res, rerr := s.handle(req)
		enc.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr})
	}
	return sc.Err()
}

func (s *Server) handle(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		v := p.ProtocolVersion
		if !supportedVersions[v] {
			v = latestVersion
		}
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "canirun", "version": s.Version},
			"instructions": "canirun answers 'will this LLM run on this machine, and how fast?'. Results labelled ESTIMATE are math from detected hardware; MEASURED are real benchmarks. " +
				"Explain verdicts in plain words. benchmark_model runs a local model for up to a minute: ask the user first. canirun never downloads models.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params"}
		}
		out, err := s.call(p.Name, p.Arguments)
		if errors.Is(err, errUnknownTool) {
			return nil, &rpcError{-32602, err.Error()}
		}
		if err != nil {
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": "error: " + err.Error()}}, "isError": true}, nil
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}, "structuredContent": out}, nil
	}
	return nil, &rpcError{-32601, "method not found: " + req.Method}
}

var errUnknownTool = errors.New("unknown tool")

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var (
	pModel = map[string]any{"type": "string", "description": "Catalog id or Ollama tag, e.g. llama3.1:8b, qwen3:30b, or a size like 13b"}
	pCtx   = map[string]any{"type": "integer", "description": "Context length in tokens (default 8192)", "minimum": 1}
	tools  = []map[string]any{
		{"name": "detect_hardware", "description": "Detect this machine's OS, CPU, RAM, GPU, usable GPU memory and memory bandwidth.", "inputSchema": obj(map[string]any{}),
			"annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false}},
		{"name": "estimate_model", "description": "ESTIMATE whether an LLM fits on this machine and how fast it will generate, for F16/Q8_0/Q5_K_M/Q4_K_M. Optional ram_gb/vram_gb/bandwidth_gbps answer 'what if I upgrade?'.",
			"inputSchema": obj(map[string]any{"model": pModel, "context": pCtx,
				"quant":          map[string]any{"type": "string", "enum": []string{"F16", "Q8_0", "Q5_K_M", "Q4_K_M"}},
				"ram_gb":         map[string]any{"type": "number", "minimum": 0},
				"vram_gb":        map[string]any{"type": "number", "minimum": 0},
				"bandwidth_gbps": map[string]any{"type": "number", "minimum": 0}}, "model"),
			"annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false}},
		{"name": "recommend_models", "description": "List catalog models that run on this machine, best first, with the quantization to use and ESTIMATED speed.",
			"inputSchema": obj(map[string]any{"context": pCtx}), "annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false}},
		{"name": "list_models", "description": "List the built-in model catalog (ids, aliases, parameter counts).",
			"inputSchema": obj(map[string]any{}), "annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false}},
		{"name": "list_installed_models", "description": "List models already pulled in the local Ollama (cloud and embedding models are flagged and cannot be benchmarked).",
			"inputSchema": obj(map[string]any{}), "annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false}},
		{"name": "benchmark_model", "description": "MEASURE real generation speed of a model already installed in local Ollama (1 warm-up + 3 runs, up to about a minute of GPU time) and compare with the estimate. Never downloads. Ask the user before running.",
			"inputSchema": obj(map[string]any{"model": map[string]any{"type": "string", "description": "Installed Ollama model name, e.g. llama3.2:latest"}}, "model"),
			"annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false, "idempotentHint": true}},
	}
)

func (s *Server) call(name string, raw json.RawMessage) (any, error) {
	var a struct {
		Model   string  `json:"model"`
		Context int     `json:"context"`
		Quant   string  `json:"quant"`
		RAMGB   float64 `json:"ram_gb"`
		VRAMGB  float64 `json:"vram_gb"`
		BWGBps  float64 `json:"bandwidth_gbps"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("bad arguments: %w", err)
		}
	}
	if a.Context == 0 {
		a.Context = 8192
	}
	machine := func() (hw.Info, error) {
		if a.RAMGB < 0 || a.VRAMGB < 0 || a.BWGBps < 0 {
			return hw.Info{}, errors.New("ram_gb, vram_gb and bandwidth_gbps must be positive")
		}
		return s.Overrides(s.Detect(), a.RAMGB, a.VRAMGB, a.BWGBps)
	}
	switch name {
	case "detect_hardware":
		return s.Detect(), nil
	case "estimate_model":
		m, err := model.Resolve(a.Model)
		if err != nil {
			return nil, err
		}
		h, err := machine()
		if err != nil {
			return nil, err
		}
		return report.BuildEstimate(m, h, a.Context, a.Quant)
	case "recommend_models":
		h, err := machine()
		if err != nil {
			return nil, err
		}
		recs, err := report.Recommend(h, a.Context)
		return map[string]any{"label": "ESTIMATE", "context": a.Context, "recommendations": recs}, err
	case "list_models":
		cat, err := model.Catalog()
		return map[string]any{"models": cat}, err
	case "list_installed_models":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list, err := s.Ollama().List(ctx)
		return map[string]any{"models": list}, err
	case "benchmark_model":
		if a.Model == "" {
			return nil, errors.New("model is required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		res, err := s.Ollama().Bench(ctx, a.Model, 3)
		if err != nil {
			return nil, err
		}
		return report.Compare(res, "", s.Detect(), a.Context), nil
	}
	return nil, fmt.Errorf("%w %q", errUnknownTool, name)
}
