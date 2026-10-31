// Command canirun tells you whether an LLM will run on your machine.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/kavinbm/canirun/internal/estimate"
	"github.com/kavinbm/canirun/internal/hw"
	"github.com/kavinbm/canirun/internal/model"
)

var version = "0.1.0-dev" // overridden by -ldflags at release time

// Exit codes are part of the CLI contract for scripts and agents.
const (
	exitOK    = 0 // success; for an estimate: the model runs here (fits or partial)
	exitError = 1 // something failed (detection, I/O, runtime)
	exitUsage = 2 // bad flags or arguments
	exitNoFit = 3 // estimate ran fine, but the model will not run on this machine
)

const usage = `canirun - will this LLM run on my machine? One command, no guessing.

Usage:
  canirun <model> [--ctx 8192] [--quant Q4_K_M] [--json]   memory estimate + verdict
  canirun hw [--json]                                       show detected hardware
  canirun list [--json]                                     built-in model catalog
  canirun recommend [--ctx 8192] [--json]                   catalog models that fit
  canirun bench [model]                                     (coming soon) measured speed via Ollama/llama.cpp
  canirun mcp                                               (coming soon) MCP server for coding agents
  canirun version

<model> is a catalog name (e.g. llama3.1:8b, qwen2.5-7b) or a size like 13b.
Results are ESTIMATES until you measure them with 'canirun bench' (coming soon).

Exit codes: 0 ok (model runs), 1 error, 2 usage error, 3 model will not fit.
`

// cliError carries an exit code with the message.
type cliError struct {
	code int
	msg  string
}

func (e *cliError) Error() string { return e.msg }

func usageErr(format string, a ...any) error {
	return &cliError{exitUsage, fmt.Sprintf(format, a...)}
}

// detect is swapped out in tests.
var detect = hw.Detect

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "hw":
		err = runHW(rest, stdout)
	case "list":
		err = runList(rest, stdout)
	case "recommend":
		err = runRecommend(rest, stdout)
	case "bench":
		runBench(stdout)
	case "mcp":
		fmt.Fprintln(stdout, "mcp: coming soon. For now, agents can call `canirun <model> --json` and `canirun hw --json`.")
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "canirun", version)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		err = runEstimate(args, stdout)
	}
	if err == nil {
		return exitOK
	}
	code := exitError
	var ce *cliError
	if errors.As(err, &ce) {
		code = ce.code
	}
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if hasJSONFlag(args) {
		printJSON(stdout, map[string]any{"error": err.Error(), "exit_code": code})
	} else if err.Error() != "" {
		fmt.Fprintln(stderr, "error:", err)
	}
	return code
}

func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "-json" {
			return true
		}
	}
	return false
}

// newFlags returns a flag set that reports errors instead of exiting.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageErr("%v", err)
	}
	if fs.NArg() > 0 {
		return usageErr("unexpected argument %q", fs.Arg(0))
	}
	return nil
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// detectChecked fails when RAM is unknown: every verdict would be a wrong "no".
func detectChecked() (hw.Info, error) {
	h := detect()
	if h.RAMBytes == 0 {
		return h, fmt.Errorf("could not detect system RAM on %s/%s, so any verdict would be wrong; please open an issue with the output of `canirun hw`", h.OS, h.Arch)
	}
	return h, nil
}

func runHW(args []string, w io.Writer) error {
	fs := newFlags("hw")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parse(fs, args); err != nil {
		return err
	}
	h := detect()
	if *asJSON {
		return printJSON(w, h)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "OS\t%s/%s\n", h.OS, h.Arch)
	fmt.Fprintf(tw, "CPU\t%s\n", orUnknown(h.CPU))
	cores := fmt.Sprintf("%d", h.Cores)
	if h.PerfCores > 0 {
		cores += fmt.Sprintf(" (%d performance + %d efficiency)", h.PerfCores, h.EffCores)
	}
	fmt.Fprintf(tw, "Cores\t%s\n", cores)
	fmt.Fprintf(tw, "RAM\t%s\n", hw.GiB(h.RAMBytes))
	gpu := orUnknown(h.GPU)
	if h.GPUCores > 0 {
		gpu += fmt.Sprintf(" (%d GPU cores)", h.GPUCores)
	}
	fmt.Fprintf(tw, "GPU\t%s\n", gpu)
	switch {
	case h.UnifiedMemory:
		b := estimate.BudgetFor(h)
		fmt.Fprintf(tw, "GPU memory\t%s unified (GPU can use ~%s)\n", hw.GiB(h.RAMBytes), hw.GiB(b.GPUBytes))
	case h.GPUMemBytes > 0:
		fmt.Fprintf(tw, "VRAM\t%s\n", hw.GiB(h.GPUMemBytes))
	default:
		fmt.Fprintf(tw, "VRAM\tnot detected\n")
	}
	tw.Flush()
	for _, n := range h.Notes {
		fmt.Fprintln(w, "note:", n)
	}
	return nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func runList(args []string, w io.Writer) error {
	fs := newFlags("list")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parse(fs, args); err != nil {
		return err
	}
	cat, err := model.Catalog()
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, cat)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPARAMS\tALIASES")
	for _, m := range cat {
		fmt.Fprintf(tw, "%s\t%s\t%gB\t%s\n", m.ID, m.Name, m.ParamsB, strings.Join(m.Aliases, ", "))
	}
	return tw.Flush()
}

type estimateOutput struct {
	Label      string            `json:"label"`
	Model      model.Model       `json:"model"`
	Context    int               `json:"context"`
	Hardware   hw.Info           `json:"hardware"`
	Budget     estimate.Budget   `json:"budget"`
	Results    []estimate.Result `json:"results"`
	Best       *estimate.Result  `json:"best,omitempty"`
	Verdict    string            `json:"verdict"`
	Warnings   []string          `json:"warnings,omitempty"`
	Assumes    string            `json:"assumes"`
	ComingSoon []string          `json:"coming_soon"`
}

func runEstimate(args []string, w io.Writer) error {
	fs := newFlags("canirun")
	ctx := fs.Int("ctx", 8192, "context length in tokens")
	quant := fs.String("quant", "", "only this quantization: F16, Q8_0, Q5_K_M, Q4_K_M")
	asJSON := fs.Bool("json", false, "print JSON")
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return usageErr("%v", err)
	}
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return usageErr("which model? e.g. `canirun llama3.1:8b` (see `canirun list`)")
	}
	if *ctx <= 0 {
		return usageErr("--ctx must be positive")
	}
	m, err := model.Resolve(name)
	if err != nil {
		return usageErr("%v", err)
	}
	h, err := detectChecked()
	if err != nil {
		return err
	}
	b := estimate.BudgetFor(h)
	results := estimate.All(m, *ctx, b)
	if *quant != "" {
		q, ok := estimate.QuantByName(strings.ToUpper(*quant))
		if !ok {
			return usageErr("unknown quant %q (use F16, Q8_0, Q5_K_M or Q4_K_M)", *quant)
		}
		results = []estimate.Result{estimate.Estimate(m, q, *ctx, b)}
	}
	out := estimateOutput{
		Label: "ESTIMATE", Model: m, Context: *ctx, Hardware: h, Budget: b, Results: results, Verdict: estimate.No,
		Assumes:    "GGUF weights, F16 KV cache, llama.cpp/Ollama-style runtime",
		ComingSoon: []string{"speed estimate (tokens/sec)", "measured benchmarks (canirun bench)", "Hugging Face model lookup"},
	}
	if m.ContextMax > 0 && *ctx > m.ContextMax {
		out.Warnings = append(out.Warnings, fmt.Sprintf("--ctx %d is above this model's maximum context of %d", *ctx, m.ContextMax))
	}
	if best, ok := estimate.Best(results); ok {
		out.Best, out.Verdict = &best, best.Verdict
	}
	if *asJSON {
		err = printJSON(w, out)
	} else {
		printEstimate(w, out)
	}
	if err == nil && out.Best == nil {
		return &cliError{exitNoFit, ""}
	}
	return err
}

var verdictText = map[string]string{
	estimate.Fits:    "✅ fits",
	estimate.Partial: "🟡 partial",
	estimate.No:      "❌ no",
}

func machineLine(h hw.Info, b estimate.Budget) string {
	name := h.GPU
	if name == "" {
		name = h.CPU
	}
	switch b.Kind {
	case "unified":
		return fmt.Sprintf("%s, %s unified memory (GPU can use ~%s)", name, hw.GiB(h.RAMBytes), hw.GiB(b.GPUBytes))
	case "discrete":
		return fmt.Sprintf("%s, %s VRAM + %s RAM", name, hw.GiB(h.GPUMemBytes), hw.GiB(h.RAMBytes))
	}
	return fmt.Sprintf("%s, %s RAM, no GPU detected (CPU only)", orUnknown(name), hw.GiB(h.RAMBytes))
}

func printEstimate(w io.Writer, o estimateOutput) {
	fmt.Fprintf(w, "ESTIMATE (not measured): %s (%gB params)\n", o.Model.Name, o.Model.ParamsB)
	fmt.Fprintf(w, "Machine: %s\n", machineLine(o.Hardware, o.Budget))
	fmt.Fprintf(w, "Context: %d tokens\n", o.Context)
	if o.Model.Generic {
		fmt.Fprintln(w, "Note: unknown architecture, KV cache is a rough guess.")
	}
	for _, warn := range o.Warnings {
		fmt.Fprintln(w, "Warning:", warn)
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "QUANT\tWEIGHTS\tKV CACHE\tOVERHEAD\tTOTAL\tVERDICT")
	for _, r := range o.Results {
		v := verdictText[r.Verdict]
		if r.Verdict == estimate.Partial {
			if r.GPUFraction > 0 {
				v += fmt.Sprintf(" (~%.0f%% on GPU, rest on CPU)", r.GPUFraction*100)
			} else {
				v += " (CPU only, slow)"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Quant, hw.GiB(r.WeightsBytes), hw.GiB(r.KVCacheBytes), hw.GiB(r.OverheadBytes), hw.GiB(r.TotalBytes), v)
	}
	tw.Flush()
	fmt.Fprintln(w)
	if o.Best != nil {
		fmt.Fprintf(w, "Best pick: %s (%s, needs ~%s)\n", o.Best.Quant, verdictText[o.Best.Verdict], hw.GiB(o.Best.TotalBytes))
	} else {
		fmt.Fprintln(w, "Verdict: ❌ won't run here at this context. Try a smaller model or --ctx 4096.")
	}
	fmt.Fprintln(w, "Speed (tokens/sec) and measured results: coming soon.")
}

func runRecommend(args []string, w io.Writer) error {
	fs := newFlags("recommend")
	ctx := fs.Int("ctx", 8192, "context length in tokens")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *ctx <= 0 {
		return usageErr("--ctx must be positive")
	}
	cat, err := model.Catalog()
	if err != nil {
		return err
	}
	h, err := detectChecked()
	if err != nil {
		return err
	}
	b := estimate.BudgetFor(h)
	type rec struct {
		Model string          `json:"model"`
		Best  estimate.Result `json:"best"`
	}
	recs := []rec{}
	for _, m := range cat {
		if best, ok := estimate.Best(estimate.All(m, *ctx, b)); ok {
			recs = append(recs, rec{m.ID, best})
		}
	}
	if *asJSON {
		return printJSON(w, map[string]any{"label": "ESTIMATE", "context": *ctx, "budget": b, "recommendations": recs})
	}
	fmt.Fprintf(w, "ESTIMATE (not measured): catalog models for %s at %d context\n\n", machineLine(h, b), *ctx)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tBEST QUANT\tNEEDS\tVERDICT")
	for _, r := range recs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Model, r.Best.Quant, hw.GiB(r.Best.TotalBytes), verdictText[r.Best.Verdict])
	}
	tw.Flush()
	if len(recs) == 0 {
		fmt.Fprintln(w, "Nothing in the catalog runs here at this context. Try --ctx 2048.")
	}
	fmt.Fprintln(w, "\nRanking by task (coding, chat, reasoning): coming soon.")
	return nil
}

func runBench(w io.Writer) {
	if p, err := exec.LookPath("ollama"); err == nil {
		fmt.Fprintf(w, "bench: coming soon. Found Ollama at %s; measured tokens/sec will use it.\n", p)
		return
	}
	fmt.Fprintln(w, "bench: coming soon. It will use Ollama or llama.cpp; neither was found on PATH.")
}
