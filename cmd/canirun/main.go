// Command canirun tells you whether an LLM will run on your machine.
package main

import (
	"encoding/json"
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

const version = "0.1.0-dev"

const usage = `canirun - will this LLM run on my machine? One command, no guessing.

Usage:
  canirun <model> [--ctx 8192] [--quant Q4_K_M] [--json]   memory estimate + verdict
  canirun hw [--json]                                       show detected hardware
  canirun list                                              built-in model catalog
  canirun recommend [--ctx 8192] [--json]                   catalog models that fit
  canirun bench [model]                                     (coming soon) measured speed via Ollama/llama.cpp
  canirun mcp                                               (coming soon) MCP server for coding agents
  canirun version

<model> is a catalog name (e.g. llama3.1:8b, qwen2.5-7b) or a size like 13b.
Results are ESTIMATES until you measure them with 'canirun bench' (coming soon).
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "hw":
		err = runHW(args)
	case "list":
		err = runList()
	case "recommend":
		err = runRecommend(args)
	case "bench":
		runBench()
	case "mcp":
		fmt.Println("mcp: coming soon. For now, agents can call `canirun <model> --json` and `canirun hw --json`.")
	case "version", "--version", "-v":
		fmt.Println("canirun", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = runEstimate(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func runHW(args []string) error {
	fs := flag.NewFlagSet("hw", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	fs.Parse(args)
	h := hw.Detect()
	if *asJSON {
		return printJSON(h)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "OS\t%s/%s\n", h.OS, h.Arch)
	fmt.Fprintf(w, "CPU\t%s\n", orUnknown(h.CPU))
	cores := fmt.Sprintf("%d", h.Cores)
	if h.PerfCores > 0 {
		cores += fmt.Sprintf(" (%d performance + %d efficiency)", h.PerfCores, h.EffCores)
	}
	fmt.Fprintf(w, "Cores\t%s\n", cores)
	fmt.Fprintf(w, "RAM\t%s\n", hw.GiB(h.RAMBytes))
	gpu := orUnknown(h.GPU)
	if h.GPUCores > 0 {
		gpu += fmt.Sprintf(" (%d GPU cores)", h.GPUCores)
	}
	fmt.Fprintf(w, "GPU\t%s\n", gpu)
	switch {
	case h.UnifiedMemory:
		b := estimate.BudgetFor(h)
		fmt.Fprintf(w, "GPU memory\t%s unified (GPU can use ~%s)\n", hw.GiB(h.RAMBytes), hw.GiB(b.GPUBytes))
	case h.GPUMemBytes > 0:
		fmt.Fprintf(w, "VRAM\t%s\n", hw.GiB(h.GPUMemBytes))
	default:
		fmt.Fprintf(w, "VRAM\tnot detected\n")
	}
	w.Flush()
	for _, n := range h.Notes {
		fmt.Println("note:", n)
	}
	return nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func runList() error {
	cat, err := model.Catalog()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPARAMS\tALIASES")
	for _, m := range cat {
		fmt.Fprintf(w, "%s\t%s\t%gB\t%s\n", m.ID, m.Name, m.ParamsB, strings.Join(m.Aliases, ", "))
	}
	return w.Flush()
}

type estimateOutput struct {
	Label      string            `json:"label"`
	Model      model.Model       `json:"model"`
	Context    int               `json:"context"`
	Hardware   hw.Info           `json:"hardware"`
	Budget     estimate.Budget   `json:"budget"`
	Results    []estimate.Result `json:"results"`
	Best       *estimate.Result  `json:"best,omitempty"`
	Assumes    string            `json:"assumes"`
	ComingSoon []string          `json:"coming_soon"`
}

func runEstimate(args []string) error {
	fs := flag.NewFlagSet("canirun", flag.ExitOnError)
	ctx := fs.Int("ctx", 8192, "context length in tokens")
	quant := fs.String("quant", "", "only this quantization: F16, Q8_0, Q5_K_M, Q4_K_M")
	asJSON := fs.Bool("json", false, "print JSON")
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	fs.Parse(args)
	if name == "" {
		name = fs.Arg(0)
	}
	if name == "" {
		return fmt.Errorf("which model? e.g. `canirun llama3.1:8b` (see `canirun list`)")
	}
	if *ctx <= 0 {
		return fmt.Errorf("--ctx must be positive")
	}
	m, err := model.Resolve(name)
	if err != nil {
		return err
	}
	h := hw.Detect()
	b := estimate.BudgetFor(h)
	results := estimate.All(m, *ctx, b)
	if *quant != "" {
		q, ok := estimate.QuantByName(strings.ToUpper(*quant))
		if !ok {
			return fmt.Errorf("unknown quant %q (use F16, Q8_0, Q5_K_M or Q4_K_M)", *quant)
		}
		results = []estimate.Result{estimate.Estimate(m, q, *ctx, b)}
	}
	out := estimateOutput{
		Label: "ESTIMATE", Model: m, Context: *ctx, Hardware: h, Budget: b, Results: results,
		Assumes:    "GGUF weights, F16 KV cache, llama.cpp/Ollama-style runtime",
		ComingSoon: []string{"speed estimate (tokens/sec)", "measured benchmarks (canirun bench)", "Hugging Face model lookup"},
	}
	if best, ok := estimate.Best(results); ok {
		out.Best = &best
	}
	if *asJSON {
		return printJSON(out)
	}
	printEstimate(os.Stdout, out)
	return nil
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

func runRecommend(args []string) error {
	fs := flag.NewFlagSet("recommend", flag.ExitOnError)
	ctx := fs.Int("ctx", 8192, "context length in tokens")
	asJSON := fs.Bool("json", false, "print JSON")
	fs.Parse(args)
	cat, err := model.Catalog()
	if err != nil {
		return err
	}
	h := hw.Detect()
	b := estimate.BudgetFor(h)
	type rec struct {
		Model string          `json:"model"`
		Best  estimate.Result `json:"best"`
	}
	var recs []rec
	for _, m := range cat {
		if best, ok := estimate.Best(estimate.All(m, *ctx, b)); ok {
			recs = append(recs, rec{m.ID, best})
		}
	}
	if *asJSON {
		return printJSON(map[string]any{"label": "ESTIMATE", "context": *ctx, "budget": b, "recommendations": recs})
	}
	fmt.Printf("ESTIMATE (not measured): catalog models for %s at %d context\n\n", machineLine(h, b), *ctx)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tBEST QUANT\tNEEDS\tVERDICT")
	for _, r := range recs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Model, r.Best.Quant, hw.GiB(r.Best.TotalBytes), verdictText[r.Best.Verdict])
	}
	tw.Flush()
	if len(recs) == 0 {
		fmt.Println("Nothing in the catalog runs here at this context. Try --ctx 2048.")
	}
	fmt.Println("\nRanking by task (coding, chat, reasoning): coming soon.")
	return nil
}

func runBench() {
	if p, err := exec.LookPath("ollama"); err == nil {
		fmt.Printf("bench: coming soon. Found Ollama at %s; measured tokens/sec will use it.\n", p)
		return
	}
	fmt.Println("bench: coming soon. It will use Ollama or llama.cpp; neither was found on PATH.")
}
