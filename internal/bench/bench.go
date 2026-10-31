// Package bench measures real generation speed through runtimes the user
// already has: Ollama (HTTP API) or llama.cpp (llama-bench). It never
// downloads a model and never sends prompts off the machine.
package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Result is one MEASURED benchmark.
type Result struct {
	Label          string  `json:"label"` // always "MEASURED"
	Runtime        string  `json:"runtime"`
	RuntimeVersion string  `json:"runtime_version,omitempty"`
	Model          string  `json:"model"`
	Family         string  `json:"family,omitempty"`
	ParamSize      string  `json:"parameter_size,omitempty"`
	Quant          string  `json:"quant,omitempty"`
	GenTPS         float64 `json:"gen_tokens_per_sec"`
	PromptTPS      float64 `json:"prompt_tokens_per_sec,omitempty"`
	LoadSec        float64 `json:"load_sec,omitempty"`
	SizeBytes      uint64  `json:"size_bytes,omitempty"`
	GPUShare       float64 `json:"gpu_share"` // share of the loaded model in GPU memory; -1 unknown
	Runs           int     `json:"runs"`
}

// ErrNoRuntime means neither Ollama nor llama.cpp is reachable.
var ErrNoRuntime = errors.New("no runtime found")

// ErrNotInstalled means the requested model is not pulled locally.
var ErrNotInstalled = errors.New("model not installed")

// Ollama talks to a local Ollama server.
type Ollama struct {
	Base   string // e.g. http://127.0.0.1:11434
	Client *http.Client
}

// NewOllama honours OLLAMA_HOST, defaulting to the local server.
func NewOllama() *Ollama {
	return &Ollama{Base: OllamaBase(os.Getenv("OLLAMA_HOST")), Client: &http.Client{Timeout: 10 * time.Minute}}
}

// OllamaBase turns an OLLAMA_HOST value into a base URL. A bind address
// like 0.0.0.0 means "this machine".
func OllamaBase(env string) string {
	if env == "" {
		return "http://127.0.0.1:11434"
	}
	if !strings.Contains(env, "://") {
		env = "http://" + env
	}
	u, err := url.Parse(env)
	if err != nil || u.Host == "" {
		return "http://127.0.0.1:11434"
	}
	host, port := u.Hostname(), u.Port()
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "11434"
	}
	return u.Scheme + "://" + host + ":" + port
}

func (o *Ollama) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, o.Base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: Ollama is not reachable at %s (is `ollama serve` running?)", ErrNoRuntime, o.Base)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotInstalled
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ollama %s: %s: %s", path, resp.Status, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Version returns the Ollama server version.
func (o *Ollama) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	err := o.do(ctx, http.MethodGet, "/api/version", nil, &v)
	return v.Version, err
}

// Installed is a locally pulled model.
type Installed struct {
	Name      string `json:"name"`
	SizeBytes uint64 `json:"size_bytes"`
	Family    string `json:"family"`
	ParamSize string `json:"parameter_size"`
	Quant     string `json:"quant"`
	// Cloud models run on ollama.com: benchmarking them would send prompts
	// off the machine and measure someone else's GPU, so they are skipped.
	Cloud bool `json:"cloud"`
	// Embedding-only models cannot generate text.
	Embedding bool `json:"embedding"`
}

// Benchable reports whether a model can be measured locally.
func (m Installed) Benchable() bool { return !m.Cloud && !m.Embedding }

// List returns locally installed models, smallest first.
func (o *Ollama) List(ctx context.Context) ([]Installed, error) {
	var tags struct {
		Models []struct {
			Name       string `json:"name"`
			Size       uint64 `json:"size"`
			RemoteHost string `json:"remote_host"`
			Details    struct {
				Family    string   `json:"family"`
				Families  []string `json:"families"`
				ParamSize string   `json:"parameter_size"`
				Quant     string   `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := o.do(ctx, http.MethodGet, "/api/tags", nil, &tags); err != nil {
		return nil, err
	}
	out := []Installed{}
	for _, m := range tags.Models {
		in := Installed{Name: m.Name, SizeBytes: m.Size, Family: m.Details.Family, ParamSize: m.Details.ParamSize, Quant: m.Details.Quant}
		in.Cloud = m.RemoteHost != "" || m.Size == 0 || strings.Contains(m.Name, "cloud")
		in.Embedding = isEmbeddingFamily(m.Details.Family) || strings.Contains(m.Name, "embed")
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SizeBytes < out[j].SizeBytes })
	return out, nil
}

func isEmbeddingFamily(f string) bool {
	switch f {
	case "bert", "nomic-bert", "xlm-roberta":
		return true
	}
	return false
}

type genResponse struct {
	EvalCount          int   `json:"eval_count"`
	EvalDuration       int64 `json:"eval_duration"`
	PromptEvalCount    int   `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	LoadDuration       int64 `json:"load_duration"`
}

func (o *Ollama) generate(ctx context.Context, model, prompt string, n int) (genResponse, error) {
	var g genResponse
	err := o.do(ctx, http.MethodPost, "/api/generate", map[string]any{
		"model": model, "prompt": prompt, "stream": false, "raw": true,
		"options": map[string]any{"num_predict": n, "temperature": 0, "seed": 42},
	}, &g)
	return g, err
}

// benchPrompt is long enough (~300 tokens) to give a meaningful prompt
// processing number. Each run gets a unique first line so Ollama's prompt
// cache cannot skip the work.
var benchPrompt = strings.Repeat("The quick brown fox jumps over the lazy dog while the lighthouse keeper writes in a logbook. ", 15) +
	"\nContinue the story in plain English:"

// Bench runs a warm-up plus `runs` measured generations of 128 tokens and
// reports the median. The model must already be pulled.
func (o *Ollama) Bench(ctx context.Context, model string, runs int) (Result, error) {
	if runs < 1 {
		runs = 1
	}
	list, err := o.List(ctx)
	if err != nil {
		return Result{}, err
	}
	var info *Installed
	for i := range list {
		if list[i].Name == model || list[i].Name == model+":latest" {
			info = &list[i]
		}
	}
	if info == nil {
		return Result{}, fmt.Errorf("%w: %q is not pulled in Ollama (canirun never downloads models; run `ollama pull %s` yourself if you want it)", ErrNotInstalled, model, model)
	}
	if info.Cloud {
		return Result{}, fmt.Errorf("%q is an Ollama cloud model; it runs on ollama.com, so canirun will not benchmark it", model)
	}
	if info.Embedding {
		return Result{}, fmt.Errorf("%q is an embedding model and cannot generate text", model)
	}
	ver, _ := o.Version(ctx)
	warm, err := o.generate(ctx, info.Name, "Hello", 1)
	if err != nil {
		return Result{}, err
	}
	var gen, pp []float64
	for i := 0; i < runs; i++ {
		g, err := o.generate(ctx, info.Name, fmt.Sprintf("Run %d at %d.\n%s", i, time.Now().UnixNano(), benchPrompt), 128)
		if err != nil {
			return Result{}, err
		}
		if g.EvalDuration > 0 {
			gen = append(gen, float64(g.EvalCount)/float64(g.EvalDuration)*1e9)
		}
		if g.PromptEvalDuration > 0 {
			pp = append(pp, float64(g.PromptEvalCount)/float64(g.PromptEvalDuration)*1e9)
		}
	}
	if len(gen) == 0 {
		return Result{}, fmt.Errorf("ollama returned no timing data for %q", model)
	}
	return Result{
		Label: "MEASURED", Runtime: "ollama", RuntimeVersion: ver, Model: info.Name, Family: info.Family,
		ParamSize: info.ParamSize, Quant: info.Quant, GenTPS: round1(median(gen)), PromptTPS: round1(median(pp)),
		LoadSec: round1(float64(warm.LoadDuration) / 1e9), SizeBytes: info.SizeBytes, GPUShare: o.gpuShare(ctx, info.Name), Runs: len(gen),
	}, nil
}

// gpuShare reads /api/ps to see how much of the loaded model sits in VRAM.
func (o *Ollama) gpuShare(ctx context.Context, model string) float64 {
	var ps struct {
		Models []struct {
			Name     string `json:"name"`
			Size     uint64 `json:"size"`
			SizeVRAM uint64 `json:"size_vram"`
		} `json:"models"`
	}
	if o.do(ctx, http.MethodGet, "/api/ps", nil, &ps) != nil {
		return -1
	}
	for _, m := range ps.Models {
		if m.Name == model && m.Size > 0 {
			return round2(float64(m.SizeVRAM) / float64(m.Size))
		}
	}
	return -1
}

// LlamaBench runs llama.cpp's llama-bench on a local GGUF file.
func LlamaBench(ctx context.Context, gguf string) (Result, error) {
	bin, err := exec.LookPath("llama-bench")
	if err != nil {
		return Result{}, fmt.Errorf("%w: llama-bench is not on PATH (install llama.cpp)", ErrNoRuntime)
	}
	if _, err := os.Stat(gguf); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotInstalled, err)
	}
	out, err := exec.CommandContext(ctx, bin, "-m", gguf, "-p", "512", "-n", "128", "-r", "3", "-o", "json").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return Result{}, fmt.Errorf("llama-bench failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return Result{}, err
	}
	return ParseLlamaBench(out, gguf)
}

// ParseLlamaBench reads `llama-bench -o json` output.
func ParseLlamaBench(data []byte, gguf string) (Result, error) {
	var rows []struct {
		BuildCommit string  `json:"build_commit"`
		ModelType   string  `json:"model_type"`
		ModelSize   uint64  `json:"model_size"`
		NPrompt     int     `json:"n_prompt"`
		NGen        int     `json:"n_gen"`
		AvgTS       float64 `json:"avg_ts"`
		NGPULayers  int     `json:"n_gpu_layers"`
		Samples     []int64 `json:"samples_ns"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return Result{}, fmt.Errorf("could not parse llama-bench output: %w", err)
	}
	r := Result{Label: "MEASURED", Runtime: "llama.cpp", Model: gguf, GPUShare: -1}
	for _, row := range rows {
		r.RuntimeVersion, r.SizeBytes = row.BuildCommit, row.ModelSize
		r.ParamSize, r.Quant = splitModelType(row.ModelType)
		if row.NGen > 0 && row.NPrompt == 0 {
			r.GenTPS, r.Runs = round1(row.AvgTS), max(len(row.Samples), 1)
		}
		if row.NPrompt > 0 && row.NGen == 0 {
			r.PromptTPS = round1(row.AvgTS)
		}
	}
	if r.GenTPS == 0 {
		return Result{}, errors.New("llama-bench output had no generation result")
	}
	return r, nil
}

// splitModelType turns "llama 3B Q4_K - Medium" into ("3B", "Q4_K - Medium").
func splitModelType(s string) (size, quant string) {
	f := strings.Fields(s)
	if len(f) >= 3 {
		return f[1], strings.Join(f[2:], " ")
	}
	return "", s
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
