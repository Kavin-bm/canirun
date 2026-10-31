package report

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kavinbm16/canirun/internal/bench"
	"github.com/kavinbm16/canirun/internal/hw"
	"github.com/kavinbm16/canirun/internal/model"
)

var m4pro = hw.Info{RAMBytes: 24 << 30, UnifiedMemory: true, GPU: "Apple M4 Pro", GPUBandwidthGBps: 273, CPUBandwidthGBps: 136, BandwidthSource: "table"}

func TestBuiltInDataIsValid(t *testing.T) {
	for _, err := range Validate(nil) {
		t.Error(err)
	}
}

func TestValidateFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("new-model.yaml", "id: new-model\nparams_b: 3\nlayers: 28\nkv_heads: 8\nhead_dim: 128\n")
	if errs := Validate([]string{good}); len(errs) != 0 {
		t.Fatalf("good file rejected: %v", errs)
	}
	bad := []string{
		write("wrong-name.yaml", "id: other\nparams_b: 3\nlayers: 28\nkv_heads: 8\nhead_dim: 128\n"),
		write("dup.yaml", "id: dup\naliases: [llama3.2]\nparams_b: 3\nlayers: 28\nkv_heads: 8\nhead_dim: 128\n"),
		write("typo.yaml", "id: typo\nparams_b: 3\nlayers: 28\nkv_head: 8\nhead_dim: 128\n"),
		write("result.yaml", "model: nope\nquant: Q3_K\nchip: X\nruntime: ollama\ngen_tps: 10\n"),
	}
	for _, p := range bad {
		if errs := Validate([]string{p}); len(errs) == 0 {
			t.Errorf("%s should fail validation", filepath.Base(p))
		}
	}
}

func TestCompareAndResultFile(t *testing.T) {
	r := bench.Result{Runtime: "ollama", RuntimeVersion: "0.21.2", Model: "llama3.2:latest", Quant: "Q4_K_M", GenTPS: 88.8, GPUShare: 1}
	c := Compare(r, "", m4pro, 8192)
	if c.CatalogID != "llama3.2-3b" || c.EstimateTPS == 0 || c.DeltaPct > 10 || c.DeltaPct < -10 {
		t.Fatalf("%+v", c)
	}
	res, file, err := ResultFile(c, m4pro, "test", "2026-10-31")
	if err != nil || file != "llama3.2-3b-q4_k_m-apple-m4-pro-ollama.yaml" {
		t.Fatalf("%v %s", err, file)
	}
	back, err := model.ParseResult(res.Format())
	if err != nil || back.GenTPS != 88.8 || back.Chip != "Apple M4 Pro" {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	if NormalizeQuant("Q4_K - Medium") != "Q4_K_M" || NormalizeQuant("bf16") != "F16" {
		t.Fatal("quant normalisation")
	}
}

func TestEstimateShowsMeasured(t *testing.T) {
	m, _ := model.Resolve("llama3.2:3b")
	e, err := BuildEstimate(m, m4pro, 8192, "")
	if err != nil || len(e.Measured) == 0 || e.Measured[0].Chip != "Apple M4 Pro" {
		t.Fatalf("%v %+v", err, e.Measured)
	}
}
