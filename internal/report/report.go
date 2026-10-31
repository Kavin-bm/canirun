// Package report builds the answers that the CLI, the local UI and the MCP
// server all show, so the three never disagree.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kavinbm16/canirun/internal/bench"
	"github.com/kavinbm16/canirun/internal/estimate"
	"github.com/kavinbm16/canirun/internal/hw"
	"github.com/kavinbm16/canirun/internal/model"
)

// Estimate is the full answer for one model on one machine.
type Estimate struct {
	Label    string            `json:"label"` // "ESTIMATE"
	Model    model.Model       `json:"model"`
	Context  int               `json:"context"`
	Hardware hw.Info           `json:"hardware"`
	Budget   estimate.Budget   `json:"budget"`
	Results  []estimate.Result `json:"results"`
	Best     *estimate.Result  `json:"best,omitempty"`
	Verdict  string            `json:"verdict"` // fits, partial, no
	// Measured are community benchmark results for this model, same chip first.
	Measured []model.Result `json:"measured"`
	Warnings []string       `json:"warnings,omitempty"`
	Assumes  string         `json:"assumes"`
}

// BuildEstimate estimates every quant (or just `quant`) for model m.
func BuildEstimate(m model.Model, h hw.Info, ctx int, quant string) (Estimate, error) {
	if ctx <= 0 {
		return Estimate{}, fmt.Errorf("context must be positive")
	}
	b := estimate.BudgetFor(h)
	results := estimate.All(m, ctx, b)
	if quant != "" {
		q, ok := estimate.QuantByName(strings.ToUpper(quant))
		if !ok {
			return Estimate{}, fmt.Errorf("unknown quant %q (use F16, Q8_0, Q5_K_M or Q4_K_M)", quant)
		}
		results = []estimate.Result{estimate.Estimate(m, q, ctx, b)}
	}
	e := Estimate{
		Label: "ESTIMATE", Model: m, Context: ctx, Hardware: h, Budget: b, Results: results, Verdict: estimate.No,
		Measured: model.ResultsFor(m.ID, h.GPU),
		Assumes:  "GGUF weights, F16 KV cache, llama.cpp/Ollama-style runtime; speed is at short context",
	}
	if e.Measured == nil {
		e.Measured = []model.Result{}
	}
	if m.ContextMax > 0 && ctx > m.ContextMax {
		e.Warnings = append(e.Warnings, fmt.Sprintf("context %d is above this model's maximum of %d", ctx, m.ContextMax))
	}
	if m.Generic {
		e.Warnings = append(e.Warnings, "unknown architecture: KV cache is a rough guess")
	}
	if h.BandwidthSource == "assumed" {
		e.Warnings = append(e.Warnings, fmt.Sprintf("memory bandwidth for this machine is not in the table, so speed assumes %.0f GB/s; run `canirun bench` to measure", max(h.GPUBandwidthGBps, h.CPUBandwidthGBps)))
	}
	if best, ok := estimate.Best(results); ok {
		e.Best, e.Verdict = &best, best.Verdict
	}
	return e, nil
}

// Rec is one recommended model.
type Rec struct {
	Model model.Model     `json:"model"`
	Best  estimate.Result `json:"best"`
}

// Recommend lists catalog models that run here, fastest-fitting first:
// models that fit fully in GPU memory, by size (bigger = smarter), then partial.
func Recommend(h hw.Info, ctx int) ([]Rec, error) {
	cat, err := model.Catalog()
	if err != nil {
		return nil, err
	}
	b := estimate.BudgetFor(h)
	recs := []Rec{}
	for _, m := range cat {
		if best, ok := estimate.Best(estimate.All(m, ctx, b)); ok {
			recs = append(recs, Rec{m, best})
		}
	}
	sort.SliceStable(recs, func(i, j int) bool {
		if (recs[i].Best.Verdict == estimate.Fits) != (recs[j].Best.Verdict == estimate.Fits) {
			return recs[i].Best.Verdict == estimate.Fits
		}
		return recs[i].Model.ParamsB > recs[j].Model.ParamsB
	})
	return recs, nil
}

// Comparison puts a measured benchmark next to the estimate for the same
// model and quant on this machine.
type Comparison struct {
	Measured    bench.Result `json:"measured"`
	CatalogID   string       `json:"catalog_id,omitempty"`
	Quant       string       `json:"quant,omitempty"`
	EstimateTPS float64      `json:"est_tokens_per_sec,omitempty"`
	// DeltaPct is (measured - estimate) / estimate * 100.
	DeltaPct float64 `json:"delta_pct,omitempty"`
	Note     string  `json:"note,omitempty"`
}

// Compare matches a benchmark to the catalog by name: `name` if given
// (needed for GGUF files), else the runtime's model name.
func Compare(r bench.Result, name string, h hw.Info, ctx int) Comparison {
	c := Comparison{Measured: r, Quant: NormalizeQuant(r.Quant)}
	if name == "" {
		name = r.Model
	}
	m, err := model.Resolve(name)
	if err != nil {
		m, err = model.Resolve(strings.Split(name, "-")[0])
	}
	if err != nil {
		c.Note = "model is not in the catalog, so there is no estimate to compare with (for a GGUF file, name it: canirun bench <catalog-model> --gguf file.gguf)"
		return c
	}
	c.CatalogID = m.ID
	q, ok := estimate.QuantByName(c.Quant)
	if !ok {
		c.Note = fmt.Sprintf("quant %q has no estimate yet", r.Quant)
		return c
	}
	est := estimate.Estimate(m, q, ctx, estimate.BudgetFor(h))
	c.EstimateTPS = est.TokensPerSec
	if est.TokensPerSec > 0 {
		c.DeltaPct = float64(int((r.GenTPS-est.TokensPerSec)/est.TokensPerSec*1000)) / 10
	}
	return c
}

// NormalizeQuant maps runtime spellings ("Q4_K - Medium", "q4_k_m") to catalog names.
func NormalizeQuant(s string) string {
	u := strings.ToUpper(strings.TrimSpace(s))
	u = strings.NewReplacer(" - MEDIUM", "_M", " - SMALL", "_S", " - LARGE", "_L").Replace(u)
	if u == "BF16" {
		return "F16"
	}
	return u
}

// ResultFile turns a comparison into a community result for models/results/.
func ResultFile(c Comparison, h hw.Info, canirunVersion, date string) (model.Result, string, error) {
	if c.CatalogID == "" {
		return model.Result{}, "", fmt.Errorf("can only save results for catalog models; add the model to models/ first")
	}
	r := model.Result{
		Model: c.CatalogID, Quant: c.Quant, Chip: h.GPU, RAMGB: float64(h.RAMBytes >> 30),
		Runtime: c.Measured.Runtime, RuntimeVersion: c.Measured.RuntimeVersion, GenTPS: c.Measured.GenTPS,
		PromptTPS: c.Measured.PromptTPS, Date: date, CanirunVersion: canirunVersion,
	}
	if r.Chip == "" {
		r.Chip = h.CPU
	}
	if c.Measured.GPUShare >= 0 {
		r.GPUShare = c.Measured.GPUShare
	}
	name := slug(fmt.Sprintf("%s-%s-%s-%s", r.Model, r.Quant, r.Chip, r.Runtime)) + ".yaml"
	return r, name, nil
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(strings.ReplaceAll(b.String(), "--", "-"), "-")
}

// Validate checks catalog and result files. With no paths it checks the
// built-in data; with paths it checks those files (for contributors' PRs).
func Validate(paths []string) []error {
	var errs []error
	cat, err := model.Catalog()
	if err != nil {
		return []error{err}
	}
	ids := map[string]bool{}
	seen := map[string]string{}
	for _, m := range cat {
		ids[m.ID] = true
		for _, k := range append([]string{m.ID}, m.Aliases...) {
			k = strings.ToLower(k)
			if other, dup := seen[k]; dup && other != m.ID {
				errs = append(errs, fmt.Errorf("%s: name %q is already used by %s", m.ID, k, other))
			}
			seen[k] = m.ID
		}
	}
	checkResult := func(where string, r model.Result) {
		if !ids[r.Model] {
			errs = append(errs, fmt.Errorf("%s: model %q is not in the catalog", where, r.Model))
		}
		if r.Runtime != "ollama" && r.Runtime != "llama.cpp" {
			errs = append(errs, fmt.Errorf("%s: runtime %q must be ollama or llama.cpp", where, r.Runtime))
		}
		if _, ok := estimate.QuantByName(r.Quant); !ok {
			errs = append(errs, fmt.Errorf("%s: quant %q is not one of F16, Q8_0, Q5_K_M, Q4_K_M", where, r.Quant))
		}
	}
	if len(paths) == 0 {
		results, err := model.Results()
		if err != nil {
			return append(errs, err)
		}
		for _, r := range results {
			checkResult(r.File, r)
		}
		return errs
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if filepath.Base(filepath.Dir(p)) == "results" || strings.Contains(string(data), "gen_tps:") {
			r, err := model.ParseResult(string(data))
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p, err))
				continue
			}
			checkResult(p, r)
			continue
		}
		m, err := model.Parse(string(data))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		if want := m.ID + ".yaml"; filepath.Base(p) != want {
			errs = append(errs, fmt.Errorf("%s: file name should be %s", p, want))
		}
		for _, k := range append([]string{m.ID}, m.Aliases...) {
			if other, dup := seen[strings.ToLower(k)]; dup && other != m.ID {
				errs = append(errs, fmt.Errorf("%s: name %q is already used by %s", p, k, other))
			}
		}
	}
	return errs
}
