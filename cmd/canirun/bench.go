package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Kavin-bm/canirun/internal/bench"
	"github.com/Kavin-bm/canirun/internal/report"
)

// newOllama is swapped out in tests.
var newOllama = bench.NewOllama

func runBench(args []string, w, stderr io.Writer) error {
	fs := newFlags("bench")
	gguf := fs.String("gguf", "", "benchmark this GGUF file with llama.cpp's llama-bench (name the catalog model as the argument)")
	runs := fs.Int("runs", 3, "measured runs (median is reported)")
	save := fs.Bool("save", false, "write a result file you can submit to models/results/")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	if *runs < 1 || *runs > 20 {
		return usageErr("--runs must be between 1 and 20")
	}
	h := detect()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var res bench.Result
	switch {
	case *gguf != "":
		if !*asJSON {
			fmt.Fprintf(stderr, "Running llama-bench on %s (this takes a minute)...\n", *gguf)
		}
		res, err = bench.LlamaBench(ctx, *gguf)
	default:
		o := newOllama()
		name := ""
		if len(pos) > 0 {
			name = pos[0]
		} else {
			name, err = pickSmallest(ctx, o)
			if err != nil {
				return runtimeErr(err)
			}
		}
		if !*asJSON {
			fmt.Fprintf(stderr, "Benchmarking %s with Ollama: 1 warm-up + %d runs of 128 tokens...\n", name, *runs)
		}
		res, err = o.Bench(ctx, name, *runs)
	}
	if err != nil {
		return runtimeErr(err)
	}
	hint := ""
	if *gguf != "" && len(pos) > 0 {
		hint = pos[0]
	}
	c := report.Compare(res, hint, h, 8192)
	if *asJSON {
		if err := printJSON(w, c); err != nil {
			return err
		}
	} else {
		printComparison(w, c)
	}
	if *save {
		r, file, err := report.ResultFile(c, h, version, time.Now().Format("2006-01-02"))
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(r.Format()), 0o644); err != nil {
			return err
		}
		if !*asJSON {
			fmt.Fprintf(w, "\nSaved %s. Share it: copy it to models/results/ in a fork and open a pull request.\n", file)
		}
	}
	return nil
}

func runtimeErr(err error) error {
	if errors.Is(err, bench.ErrNoRuntime) || errors.Is(err, bench.ErrNotInstalled) {
		return &cliError{exitNoRuntime, err.Error()}
	}
	return err
}

// pickSmallest chooses the smallest locally installed text model.
func pickSmallest(ctx context.Context, o *bench.Ollama) (string, error) {
	list, err := o.List(ctx)
	if err != nil {
		return "", err
	}
	for _, m := range list {
		if m.Benchable() {
			return m.Name, nil
		}
	}
	return "", fmt.Errorf("%w: no local text models in Ollama (cloud and embedding models are skipped). Pull one yourself, e.g. `ollama pull llama3.2`", bench.ErrNotInstalled)
}

func printComparison(w io.Writer, c report.Comparison) {
	m := c.Measured
	fmt.Fprintf(w, "MEASURED: %s (%s %s) via %s %s\n", m.Model, m.ParamSize, m.Quant, m.Runtime, m.RuntimeVersion)
	fmt.Fprintf(w, "  Generation:        %.1f tok/s (%s)\n", m.GenTPS, feel(m.GenTPS))
	if m.PromptTPS > 0 {
		fmt.Fprintf(w, "  Prompt processing: %.0f tok/s\n", m.PromptTPS)
	}
	if m.LoadSec > 0 {
		fmt.Fprintf(w, "  Load time:         %.1f s\n", m.LoadSec)
	}
	if m.GPUShare >= 0 {
		fmt.Fprintf(w, "  On GPU:            %.0f%%\n", m.GPUShare*100)
	}
	if c.EstimateTPS > 0 {
		fmt.Fprintf(w, "ESTIMATE for %s %s on this machine: %.1f tok/s (measured is %+.0f%%)\n", c.CatalogID, c.Quant, c.EstimateTPS, c.DeltaPct)
	} else if c.Note != "" {
		fmt.Fprintln(w, "Note:", c.Note)
	}
}
