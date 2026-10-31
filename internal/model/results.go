package model

import (
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/kavinbm/canirun/models"
)

// Result is a MEASURED benchmark contributed to models/results/*.yaml,
// usually written by `canirun bench --save`.
type Result struct {
	File           string  `json:"file,omitempty"`
	Model          string  `json:"model"` // catalog id
	Quant          string  `json:"quant"`
	Chip           string  `json:"chip"` // GPU or Apple chip name as `canirun hw` prints it
	RAMGB          float64 `json:"ram_gb,omitempty"`
	Runtime        string  `json:"runtime"`
	RuntimeVersion string  `json:"runtime_version,omitempty"`
	GenTPS         float64 `json:"gen_tokens_per_sec"`
	PromptTPS      float64 `json:"prompt_tokens_per_sec,omitempty"`
	GPUShare       float64 `json:"gpu_share,omitempty"`
	Date           string  `json:"date,omitempty"`
	CanirunVersion string  `json:"canirun_version,omitempty"`
}

// ParseResult reads one result file. Unknown keys are errors.
func ParseResult(data string) (Result, error) {
	var r Result
	err := eachKV(data, func(k, v string) error {
		var err error
		switch k {
		case "model":
			r.Model = v
		case "quant":
			r.Quant = v
		case "chip":
			r.Chip = v
		case "ram_gb":
			r.RAMGB, err = strconv.ParseFloat(v, 64)
		case "runtime":
			r.Runtime = v
		case "runtime_version":
			r.RuntimeVersion = v
		case "gen_tps":
			r.GenTPS, err = strconv.ParseFloat(v, 64)
		case "prompt_tps":
			r.PromptTPS, err = strconv.ParseFloat(v, 64)
		case "gpu_share":
			r.GPUShare, err = strconv.ParseFloat(v, 64)
		case "date":
			r.Date = v
		case "canirun_version":
			r.CanirunVersion = v
		default:
			return fmt.Errorf("unknown field %q", k)
		}
		return err
	})
	if err != nil {
		return r, err
	}
	if r.Model == "" || r.Quant == "" || r.Chip == "" || r.Runtime == "" || r.GenTPS <= 0 {
		return r, fmt.Errorf("model, quant, chip, runtime and gen_tps are required")
	}
	if r.GenTPS > 5000 || r.PromptTPS < 0 || r.GPUShare < 0 || r.GPUShare > 1 {
		return r, fmt.Errorf("gen_tps, prompt_tps or gpu_share look wrong")
	}
	return r, nil
}

// Format writes a result in the flat YAML format.
func (r Result) Format() string {
	var b strings.Builder
	b.WriteString("# yaml-language-server: $schema=../result.schema.json\n# MEASURED with canirun bench. Submit by pull request to models/results/.\n")
	kv := func(k, v string) {
		if v != "" && v != "0" {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	f := func(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }
	kv("model", r.Model)
	kv("quant", r.Quant)
	kv("chip", r.Chip)
	kv("ram_gb", f(r.RAMGB))
	kv("runtime", r.Runtime)
	kv("runtime_version", r.RuntimeVersion)
	kv("gen_tps", f(r.GenTPS))
	kv("prompt_tps", f(r.PromptTPS))
	kv("gpu_share", f(r.GPUShare))
	kv("date", r.Date)
	kv("canirun_version", r.CanirunVersion)
	return b.String()
}

// Results returns every built-in measured result.
func Results() ([]Result, error) {
	files, err := fs.Glob(models.FS, "results/*.yaml")
	if err != nil {
		return nil, err
	}
	out := []Result{}
	for _, f := range files {
		b, err := models.FS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		r, err := ParseResult(string(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		r.File = path.Base(f)
		out = append(out, r)
	}
	return out, nil
}

// ResultsFor returns measured results for a model, those on `chip` first.
func ResultsFor(modelID, chip string) []Result {
	all, _ := Results()
	var same, other []Result
	for _, r := range all {
		if r.Model != modelID {
			continue
		}
		if strings.EqualFold(r.Chip, chip) {
			same = append(same, r)
		} else {
			other = append(other, r)
		}
	}
	return append(same, other...)
}
