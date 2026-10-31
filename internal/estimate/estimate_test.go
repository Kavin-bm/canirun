package estimate

import (
	"math"
	"testing"

	"github.com/kavinbm16/canirun/internal/hw"
	"github.com/kavinbm16/canirun/internal/model"
)

var llama8b = model.Model{ID: "llama3.1-8b", ParamsB: 8.03, Layers: 32, KVHeads: 8, HeadDim: 128}

func near(t *testing.T, name string, got uint64, want float64, tolPct float64) {
	t.Helper()
	if math.Abs(float64(got)-want)/want*100 > tolPct {
		t.Errorf("%s = %d, want ~%.0f", name, got, want)
	}
}

func TestWeights(t *testing.T) {
	q4, _ := QuantByName("Q4_K_M")
	f16, _ := QuantByName("F16")
	q8, _ := QuantByName("Q8_0")
	// 8.03e9 * 4.85 / 8 = 4.868e9 bytes (Ollama's llama3.1:8b Q4_K_M is ~4.9 GB)
	near(t, "Q4_K_M", WeightsBytes(8.03, q4), 4.868e9, 0.1)
	near(t, "F16", WeightsBytes(8.03, f16), 16.06e9, 0.1)
	near(t, "Q8_0", WeightsBytes(8.03, q8), 8.532e9, 0.1)
}

func TestKVCache(t *testing.T) {
	// 2 * 32 layers * 8 heads * 128 dim * 8192 ctx * 2 bytes = exactly 1 GiB
	if got := KVCacheBytes(llama8b, 8192); got != 1<<30 {
		t.Fatalf("kv = %d, want %d", got, 1<<30)
	}
	// Generic 8B guess matches Llama 3.1 8B per token.
	if got := KVCacheBytes(model.Generic(8), 8192); got != 1<<30 {
		t.Fatalf("generic kv = %d", got)
	}
	// Doubling context doubles KV.
	if KVCacheBytes(llama8b, 16384) != 2*KVCacheBytes(llama8b, 8192) {
		t.Fatal("kv not linear in context")
	}
}

func TestOverhead(t *testing.T) {
	if got := OverheadBytes(1e9); got != 512<<20+5e7 {
		t.Fatalf("overhead %d", got)
	}
}

func TestBudget(t *testing.T) {
	mac16 := BudgetFor(hw.Info{RAMBytes: 16 * GiB, UnifiedMemory: true})
	near(t, "mac16 gpu", mac16.GPUBytes, 16*GiB*2.0/3.0, 0.01)
	mac64 := BudgetFor(hw.Info{RAMBytes: 64 * GiB, UnifiedMemory: true})
	near(t, "mac64 gpu", mac64.GPUBytes, 48*GiB, 0.01)
	cpu := BudgetFor(hw.Info{RAMBytes: 32 * GiB})
	if cpu.GPUBytes != 0 || cpu.Kind != "cpu" {
		t.Fatalf("cpu budget %+v", cpu)
	}
}

func TestVerdicts(t *testing.T) {
	q4, _ := QuantByName("Q4_K_M")
	f16, _ := QuantByName("F16")
	mac16 := BudgetFor(hw.Info{RAMBytes: 16 * GiB, UnifiedMemory: true})
	if r := Estimate(llama8b, q4, 8192, mac16); r.Verdict != Fits {
		t.Errorf("8B Q4 on 16GB Mac: %+v", r)
	}
	if r := Estimate(llama8b, f16, 8192, mac16); r.Verdict != No {
		t.Errorf("8B F16 on 16GB Mac should not fit: %+v", r)
	}
	gpu8 := BudgetFor(hw.Info{RAMBytes: 32 * GiB, GPUMemBytes: 8 * GiB})
	r := Estimate(llama8b, f16, 8192, gpu8)
	if r.Verdict != Partial || r.GPUFraction <= 0 || r.GPUFraction >= 1 {
		t.Errorf("8B F16 on 8GB GPU + 32GB RAM should be partial: %+v", r)
	}
	big := model.Model{ParamsB: 70.6, Layers: 80, KVHeads: 8, HeadDim: 128}
	if r := Estimate(big, q4, 8192, mac16); r.Verdict != No {
		t.Errorf("70B on 16GB: %+v", r)
	}
}

func TestBest(t *testing.T) {
	mac16 := BudgetFor(hw.Info{RAMBytes: 16 * GiB, UnifiedMemory: true})
	best, ok := Best(All(llama8b, 8192, mac16))
	if !ok || best.Quant != "Q8_0" {
		t.Fatalf("best = %+v ok=%v", best, ok)
	}
	// CPU only: Q8_0 would crawl, so the faster Q4_K_M wins.
	cpu := BudgetFor(hw.Info{RAMBytes: 16 * GiB, CPUBandwidthGBps: 50})
	if best, _ := Best(All(llama8b, 8192, cpu)); best.Quant != "Q4_K_M" {
		t.Fatalf("cpu best = %+v", best)
	}
	tiny := BudgetFor(hw.Info{RAMBytes: 2 * GiB})
	if _, ok := Best(All(llama8b, 8192, tiny)); ok {
		t.Fatal("nothing should run on 2GB")
	}
}

func TestTokensPerSec(t *testing.T) {
	q4, _ := QuantByName("Q4_K_M")
	m4pro := BudgetFor(hw.Info{RAMBytes: 24 * GiB, UnifiedMemory: true, GPUBandwidthGBps: 273, CPUBandwidthGBps: 136})
	llama3b := model.Model{ParamsB: 3.21, Layers: 28, KVHeads: 8, HeadDim: 128}
	// Calibration point: measured 93 tok/s with Ollama on an M4 Pro.
	if got := Estimate(llama3b, q4, 8192, m4pro).TokensPerSec; math.Abs(got-93) > 3 {
		t.Errorf("llama3.2 3B on M4 Pro: %v tok/s, want ~93", got)
	}
	// MoE: speed follows active params, memory follows total params.
	moe := model.Model{ParamsB: 30.5, ActiveParamsB: 3.3, Layers: 48, KVHeads: 4, HeadDim: 128}
	dense := moe
	dense.ActiveParamsB = 0
	if TokensPerSec(moe, q4, 1, m4pro) < 5*TokensPerSec(dense, q4, 1, m4pro) {
		t.Error("MoE should be much faster than a dense model of the same size")
	}
	// Partial offload is slower than all-GPU and faster than CPU only.
	full, part, cpu := TokensPerSec(llama8b, q4, 1, m4pro), TokensPerSec(llama8b, q4, 0.5, m4pro), TokensPerSec(llama8b, q4, 0, m4pro)
	if !(full > part && part > cpu && cpu > 0) {
		t.Errorf("full %v, partial %v, cpu %v", full, part, cpu)
	}
	if r := Estimate(llama8b, q4, 8192, BudgetFor(hw.Info{RAMBytes: 1 * GiB})); r.TokensPerSec != 0 {
		t.Errorf("no-fit must have 0 tok/s: %+v", r)
	}
}

func TestWiredLimit(t *testing.T) {
	b := BudgetFor(hw.Info{RAMBytes: 24 * GiB, UnifiedMemory: true, GPUWiredLimitBytes: 20 * GiB})
	if b.GPUBytes != 20*GiB {
		t.Fatalf("wired limit ignored: %+v", b)
	}
}
