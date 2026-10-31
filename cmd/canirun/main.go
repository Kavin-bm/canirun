// Command canirun tells you whether an LLM will run on your machine, and how fast.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Kavin-bm/canirun/internal/estimate"
	"github.com/Kavin-bm/canirun/internal/hw"
	"github.com/Kavin-bm/canirun/internal/model"
	"github.com/Kavin-bm/canirun/internal/report"
)

var version = "0.1.0-dev" // overridden by -ldflags at release time

// Exit codes are part of the CLI contract for scripts and agents.
const (
	exitOK        = 0 // success; for an estimate: the model runs here (fits or partial)
	exitError     = 1 // something failed (detection, I/O)
	exitUsage     = 2 // bad flags or arguments
	exitNoFit     = 3 // estimate ran fine, but the model will not run on this machine
	exitNoRuntime = 4 // bench: Ollama/llama.cpp not available, or the model is not installed
)

const usage = `canirun - will this LLM run on my machine, and how fast?

Usage:
  canirun <model> [flags]          memory + speed ESTIMATE and a verdict for each quantization
  canirun recommend [flags]        catalog models that run here, best first
  canirun bench [ollama-model]     MEASURE real speed with Ollama (never downloads models)
  canirun bench <model> --gguf f   MEASURE a GGUF file with llama.cpp's llama-bench
  canirun hw                       detected hardware
  canirun list                     built-in model catalog
  canirun ui [--port 7878]         local web UI on 127.0.0.1
  canirun mcp                      MCP server (stdio) for coding agents
  canirun validate [files...]      check catalog / result YAML files
  canirun version

Estimate flags:
  --ctx 8192      context length in tokens
  --quant Q4_K_M  only this quantization (F16, Q8_0, Q5_K_M, Q4_K_M)
  --ram 64        pretend this much system RAM (GB)   - "what if I upgrade?"
  --vram 24       pretend a GPU with this much VRAM (GB)
  --bw 1008       memory bandwidth in GB/s for the speed estimate
  --json          machine-readable output (every command supports it)

<model> is a catalog name or Ollama tag (llama3.1:8b, qwen3:30b) or a size like 13b.
Numbers are labelled ESTIMATE or MEASURED. Nothing leaves your machine.

Exit codes: 0 ok (model runs), 1 error, 2 usage error, 3 model will not fit,
            4 runtime or model not available for bench.
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
		err = runBench(rest, stdout, stderr)
	case "validate":
		err = runValidate(rest, stdout)
	case "mcp":
		err = runMCP(rest, os.Stdin, stdout)
	case "ui":
		err = runUI(rest, stdout)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "canirun", version)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		err = runEstimate(args, stdout)
	}
	if err == nil || errors.Is(err, flag.ErrHelp) {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
		}
		return exitOK
	}
	code := exitError
	var ce *cliError
	if errors.As(err, &ce) {
		code = ce.code
	}
	if hasJSONFlag(args) {
		if err.Error() != "" {
			printJSON(stdout, map[string]any{"error": err.Error(), "exit_code": code})
		}
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

// parse parses flags that may come before or after one optional positional
// argument, which it returns.
func parse(fs *flag.FlagSet, args []string, maxPositional int) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usageErr("%v", err)
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(pos) > maxPositional {
		return nil, usageErr("unexpected argument %q", pos[maxPositional])
	}
	return pos, nil
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// hwFlags adds the "what if" hardware overrides.
type hwFlags struct{ ram, vram, bw *float64 }

func addHWFlags(fs *flag.FlagSet) hwFlags {
	return hwFlags{
		ram:  fs.Float64("ram", 0, "pretend this much system RAM (GB)"),
		vram: fs.Float64("vram", 0, "pretend a discrete GPU with this much VRAM (GB)"),
		bw:   fs.Float64("bw", 0, "memory bandwidth (GB/s) for the speed estimate"),
	}
}

// machine detects hardware, applies overrides and refuses to continue when
// RAM is unknown: every verdict would be a wrong "no".
func (f hwFlags) machine() (hw.Info, error) {
	if *f.ram < 0 || *f.vram < 0 || *f.bw < 0 {
		return hw.Info{}, usageErr("--ram, --vram and --bw must be positive")
	}
	h := detect()
	return applyOverrides(h, *f.ram, *f.vram, *f.bw)
}

func applyOverrides(h hw.Info, ram, vram, bw float64) (hw.Info, error) {
	if ram > 0 {
		h.RAMBytes = uint64(ram * (1 << 30))
		if h.UnifiedMemory {
			h.GPUMemBytes = h.RAMBytes
		}
	}
	if vram > 0 {
		h.UnifiedMemory, h.GPUMemBytes, h.GPUWiredLimitBytes = false, uint64(vram*(1<<30)), 0
		if !strings.Contains(strings.ToLower(h.GPU), "nvidia") && !strings.Contains(strings.ToLower(h.GPU), "amd") {
			h.GPU = fmt.Sprintf("hypothetical %.0f GB GPU", vram)
		}
	}
	if ram > 0 || vram > 0 || bw > 0 {
		h.Notes = append(h.Notes, "hardware overridden on the command line")
		if bw > 0 {
			h.BandwidthSource = "override"
			h.CPUBandwidthGBps = bw
			if h.UnifiedMemory || h.GPUMemBytes > 0 {
				h.GPUBandwidthGBps = bw
			}
		} else {
			h.BandwidthSource = ""
		}
		hw.Finish(&h)
	}
	if h.RAMBytes == 0 {
		return h, fmt.Errorf("could not detect system RAM on %s/%s, so any verdict would be wrong; pass --ram <GB> or open an issue with the output of `canirun hw`", h.OS, h.Arch)
	}
	return h, nil
}

func runHW(args []string, w io.Writer) error {
	fs := newFlags("hw")
	asJSON := fs.Bool("json", false, "print JSON")
	if _, err := parse(fs, args, 0); err != nil {
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
	fmt.Fprintf(tw, "Bandwidth\t%s\n", bandwidthLine(h))
	tw.Flush()
	for _, n := range h.Notes {
		fmt.Fprintln(w, "note:", n)
	}
	return nil
}

func bandwidthLine(h hw.Info) string {
	src := map[string]string{"table": "spec sheet", "assumed": "ASSUMED, chip not in table", "override": "from --bw"}[h.BandwidthSource]
	if h.GPUBandwidthGBps > 0 {
		return fmt.Sprintf("%.0f GB/s GPU (%s)", h.GPUBandwidthGBps, src)
	}
	return fmt.Sprintf("%.0f GB/s RAM (%s)", h.CPUBandwidthGBps, src)
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
	if _, err := parse(fs, args, 0); err != nil {
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
		p := fmt.Sprintf("%gB", m.ParamsB)
		if m.ActiveParamsB > 0 {
			p += fmt.Sprintf(" (%gB active)", m.ActiveParamsB)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.ID, m.Name, p, strings.Join(m.Aliases, ", "))
	}
	return tw.Flush()
}

func runEstimate(args []string, w io.Writer) error {
	fs := newFlags("canirun")
	ctx := fs.Int("ctx", 8192, "context length in tokens")
	quant := fs.String("quant", "", "only this quantization")
	asJSON := fs.Bool("json", false, "print JSON")
	hf := addHWFlags(fs)
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usageErr("which model? e.g. `canirun llama3.1:8b` (see `canirun list`)")
	}
	if *ctx <= 0 {
		return usageErr("--ctx must be positive")
	}
	m, err := model.Resolve(pos[0])
	if err != nil {
		return usageErr("%v", err)
	}
	h, err := hf.machine()
	if err != nil {
		return err
	}
	e, err := report.BuildEstimate(m, h, *ctx, *quant)
	if err != nil {
		return usageErr("%v", err)
	}
	if *asJSON {
		err = printJSON(w, e)
	} else {
		printEstimate(w, e)
	}
	if err == nil && e.Best == nil {
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

func speedText(tps float64) string {
	switch {
	case tps <= 0:
		return "-"
	case tps < 10:
		return fmt.Sprintf("~%.1f tok/s", tps)
	}
	return fmt.Sprintf("~%.0f tok/s", tps)
}

// feel turns tokens/sec into words people understand.
func feel(tps float64) string {
	switch {
	case tps >= 30:
		return "faster than you can read"
	case tps >= 10:
		return "comfortable reading speed"
	case tps >= 4:
		return "usable, but you will wait"
	case tps > 0:
		return "painfully slow"
	}
	return ""
}

func printEstimate(w io.Writer, e report.Estimate) {
	params := fmt.Sprintf("%gB params", e.Model.ParamsB)
	if e.Model.ActiveParamsB > 0 {
		params += fmt.Sprintf(", %gB active", e.Model.ActiveParamsB)
	}
	fmt.Fprintf(w, "ESTIMATE (not measured): %s (%s)\n", e.Model.Name, params)
	fmt.Fprintf(w, "Machine: %s\n", machineLine(e.Hardware, e.Budget))
	fmt.Fprintf(w, "Context: %d tokens\n", e.Context)
	for _, warn := range e.Warnings {
		fmt.Fprintln(w, "Warning:", warn)
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "QUANT\tNEEDS\tSPEED (est.)\tVERDICT")
	for _, r := range e.Results {
		v := verdictText[r.Verdict]
		if r.Verdict == estimate.Partial {
			if r.GPUFraction > 0 {
				v += fmt.Sprintf(" (~%.0f%% on GPU, rest on CPU)", r.GPUFraction*100)
			} else {
				v += " (CPU only)"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Quant, hw.GiB(r.TotalBytes), speedText(r.TokensPerSec), v)
	}
	tw.Flush()
	fmt.Fprintln(w)
	if e.Best != nil {
		b := e.Best
		fmt.Fprintf(w, "Best pick: %s - %s, needs ~%s, %s (%s).\n", b.Quant, verdictText[b.Verdict], hw.GiB(b.TotalBytes), speedText(b.TokensPerSec), feel(b.TokensPerSec))
	} else {
		fmt.Fprintln(w, "Verdict: ❌ won't run here at this context. Try a smaller model, or --ctx 4096.")
	}
	for i, r := range e.Measured {
		if i == 3 {
			break
		}
		where := "same chip as yours"
		if !strings.EqualFold(r.Chip, e.Hardware.GPU) {
			where = "different hardware"
		}
		fmt.Fprintf(w, "MEASURED by the community on %s (%s), %s %s: %.1f tok/s at %s\n", r.Chip, where, r.Runtime, r.RuntimeVersion, r.GenTPS, r.Quant)
	}
	fmt.Fprintln(w, "Memory = weights + KV cache + runtime overhead. Measure yours: canirun bench")
}

func runRecommend(args []string, w io.Writer) error {
	fs := newFlags("recommend")
	ctx := fs.Int("ctx", 8192, "context length in tokens")
	asJSON := fs.Bool("json", false, "print JSON")
	hf := addHWFlags(fs)
	if _, err := parse(fs, args, 0); err != nil {
		return err
	}
	if *ctx <= 0 {
		return usageErr("--ctx must be positive")
	}
	h, err := hf.machine()
	if err != nil {
		return err
	}
	recs, err := report.Recommend(h, *ctx)
	if err != nil {
		return err
	}
	b := estimate.BudgetFor(h)
	if *asJSON {
		err = printJSON(w, map[string]any{"label": "ESTIMATE", "context": *ctx, "hardware": h, "budget": b, "recommendations": recs})
	} else {
		fmt.Fprintf(w, "ESTIMATE (not measured): models for %s at %d context\n\n", machineLine(h, b), *ctx)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "MODEL\tBEST QUANT\tNEEDS\tSPEED (est.)\tVERDICT")
		for _, r := range recs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Model.ID, r.Best.Quant, hw.GiB(r.Best.TotalBytes), speedText(r.Best.TokensPerSec), verdictText[r.Best.Verdict])
		}
		tw.Flush()
		if len(recs) == 0 {
			fmt.Fprintln(w, "Nothing in the catalog runs here at this context. Try --ctx 2048.")
		}
	}
	if err == nil && len(recs) == 0 {
		return &cliError{exitNoFit, ""}
	}
	return err
}

func runValidate(args []string, w io.Writer) error {
	fs := newFlags("validate")
	asJSON := fs.Bool("json", false, "print JSON")
	files, err := parse(fs, args, 1<<16)
	if err != nil {
		return err
	}
	errs := report.Validate(files)
	msgs := []string{}
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	if *asJSON {
		printJSON(w, map[string]any{"ok": len(errs) == 0, "errors": msgs})
	} else {
		for _, m := range msgs {
			fmt.Fprintln(w, "✗", m)
		}
		if len(errs) == 0 {
			what := "built-in catalog and results"
			if len(files) > 0 {
				what = fmt.Sprintf("%d file(s)", len(files))
			}
			fmt.Fprintln(w, "✓", what, "valid")
		}
	}
	if len(errs) > 0 {
		return &cliError{exitError, ""}
	}
	return nil
}
