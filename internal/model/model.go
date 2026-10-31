// Package model loads the model catalog and resolves names like "llama3.1:8b".
package model

import (
	"bufio"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kavinbm/canirun/models"
)

// Model is the architecture info needed for the memory estimate.
type Model struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	ParamsB float64  `json:"params_b"`
	// ActiveParamsB is the parameters read per token for mixture-of-experts
	// models (0 = dense, all ParamsB are read). It drives the speed estimate.
	ActiveParamsB float64 `json:"active_params_b,omitempty"`
	Layers        int     `json:"layers,omitempty"`
	KVHeads       int     `json:"kv_heads,omitempty"`
	HeadDim       int     `json:"head_dim,omitempty"`
	ContextMax    int     `json:"context_max,omitempty"`
	HFRepo        string  `json:"hf_repo,omitempty"`
	// Generic is true when only a parameter count is known (KV cache is a rough guess).
	Generic bool `json:"generic,omitempty"`
}

// eachKV walks the flat "key: value" YAML subset used by models/*.yaml and
// models/results/*.yaml. Keeping the format flat keeps canirun dependency-free.
func eachKV(data string, fn func(k, v string) error) error {
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("bad line %q", line)
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if err := fn(k, v); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	return sc.Err()
}

// Parse reads one catalog file.
func Parse(data string) (Model, error) {
	var m Model
	err := eachKV(data, func(k, v string) error {
		var err error
		switch k {
		case "id":
			m.ID = v
		case "name":
			m.Name = v
		case "aliases":
			for _, a := range strings.Split(strings.Trim(v, "[]"), ",") {
				if a = strings.TrimSpace(a); a != "" {
					m.Aliases = append(m.Aliases, a)
				}
			}
		case "params_b":
			m.ParamsB, err = strconv.ParseFloat(v, 64)
		case "active_params_b":
			m.ActiveParamsB, err = strconv.ParseFloat(v, 64)
		case "layers":
			m.Layers, err = strconv.Atoi(v)
		case "kv_heads":
			m.KVHeads, err = strconv.Atoi(v)
		case "head_dim":
			m.HeadDim, err = strconv.Atoi(v)
		case "context_max":
			m.ContextMax, err = strconv.Atoi(v)
		case "hf_repo":
			m.HFRepo = v
		default:
			// Typos like "kv_head" must fail loudly, not be silently ignored.
			return fmt.Errorf("unknown field %q", k)
		}
		return err
	})
	if err != nil {
		return m, err
	}
	if m.ID == "" || m.ParamsB <= 0 || m.Layers <= 0 || m.KVHeads <= 0 || m.HeadDim <= 0 {
		return m, fmt.Errorf("model %q: id, params_b, layers, kv_heads and head_dim are required", m.ID)
	}
	if m.ActiveParamsB < 0 || m.ActiveParamsB > m.ParamsB {
		return m, fmt.Errorf("model %q: active_params_b must be between 0 and params_b", m.ID)
	}
	if m.Layers > 500 || m.KVHeads > 256 || m.HeadDim > 1024 || m.ParamsB > 10000 {
		return m, fmt.Errorf("model %q: layers, kv_heads, head_dim or params_b look wrong", m.ID)
	}
	return m, nil
}

// ActiveB is the billions of parameters read for each generated token.
func (m Model) ActiveB() float64 {
	if m.ActiveParamsB > 0 {
		return m.ActiveParamsB
	}
	return m.ParamsB
}

// Catalog returns every built-in model, sorted by size.
func Catalog() ([]Model, error) {
	files, err := fs.Glob(models.FS, "*.yaml")
	if err != nil {
		return nil, err
	}
	var out []Model
	for _, f := range files {
		b, err := models.FS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		m, err := Parse(string(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ParamsB < out[j].ParamsB })
	return out, nil
}

var sizeRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)b$`)

// Resolve finds a model by id or alias (case-insensitive), or treats "13b"
// as a generic 13-billion-parameter model.
func Resolve(name string) (Model, error) {
	n := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ":latest")
	cat, err := Catalog()
	if err != nil {
		return Model{}, err
	}
	for _, m := range cat {
		if strings.ToLower(m.ID) == n {
			return m, nil
		}
		for _, a := range m.Aliases {
			if strings.ToLower(a) == n {
				return m, nil
			}
		}
	}
	if s := sizeRe.FindStringSubmatch(n); s != nil {
		if p, _ := strconv.ParseFloat(s[1], 64); p > 0 && p <= 10000 {
			return Generic(p), nil
		}
		return Model{}, fmt.Errorf("model size %q is out of range (use something like 7b or 70b)", name)
	}
	return Model{}, fmt.Errorf("unknown model %q; run `canirun list` to see the catalog, or pass a size like `canirun 13b`", name)
}

// Generic builds a model from a parameter count only.
func Generic(paramsB float64) Model {
	return Model{ID: fmt.Sprintf("%gb", paramsB), Name: fmt.Sprintf("generic %gB model", paramsB), ParamsB: paramsB, Generic: true}
}
