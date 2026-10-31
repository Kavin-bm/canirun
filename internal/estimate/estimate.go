// Package estimate does the memory math: weights + KV cache + overhead,
// compared against what the machine can give the model.
//
// All results are ESTIMATES. Measured numbers (canirun bench) are coming soon.
package estimate

import (
	"math"

	"github.com/kavinbm/canirun/internal/hw"
	"github.com/kavinbm/canirun/internal/model"
)

// Quant is a llama.cpp/GGUF quantization type.
type Quant struct {
	Name string  `json:"name"`
	Bits float64 `json:"bits_per_weight"` // effective, incl. scales
}

// Quants, best quality first. Bits per weight are the effective averages
// llama.cpp reports for Llama-style models.
var Quants = []Quant{
	{"F16", 16},
	{"Q8_0", 8.5},
	{"Q5_K_M", 5.69},
	{"Q4_K_M", 4.85},
}

// QuantByName returns the quant with that name (case-sensitive) or false.
func QuantByName(name string) (Quant, bool) {
	for _, q := range Quants {
		if q.Name == name {
			return q, true
		}
	}
	return Quant{}, false
}

const (
	GiB = 1 << 30
	// Runtime overhead: compute buffers, CUDA/Metal context, tokenizer.
	overheadFixed = 512 << 20
	overheadFrac  = 0.05
	kvBytesPerElt = 2 // F16 KV cache
)

// Verdicts.
const (
	Fits    = "fits"    // whole model in GPU / unified memory
	Partial = "partial" // needs CPU offload or CPU-only: works, but slower
	No      = "no"      // not enough memory
)

// Result is one quantization's estimate.
type Result struct {
	Quant         string  `json:"quant"`
	WeightsBytes  uint64  `json:"weights_bytes"`
	KVCacheBytes  uint64  `json:"kv_cache_bytes"`
	OverheadBytes uint64  `json:"overhead_bytes"`
	TotalBytes    uint64  `json:"total_bytes"`
	Verdict       string  `json:"verdict"`
	GPUFraction   float64 `json:"gpu_fraction"` // share of the model that fits in GPU memory
}

// Budget is how much memory the machine can give a model.
type Budget struct {
	GPUBytes   uint64 `json:"gpu_bytes"`   // fast memory (VRAM or GPU share of unified memory)
	TotalBytes uint64 `json:"total_bytes"` // GPU + usable system RAM
	Kind       string `json:"kind"`        // unified, discrete, cpu
}

// WeightsBytes = params * bits / 8.
func WeightsBytes(paramsB float64, q Quant) uint64 {
	return uint64(paramsB * 1e9 * q.Bits / 8)
}

// KVCacheBytes = 2 (K and V) * layers * kv_heads * head_dim * context * 2 bytes.
// For generic models (no architecture known) it scales a Llama-3-8B-like
// 128 KiB/token by sqrt(params/8): a rough guess.
func KVCacheBytes(m model.Model, ctx int) uint64 {
	if m.Generic || m.Layers == 0 {
		perTok := 128 * 1024 * math.Sqrt(m.ParamsB/8)
		return uint64(perTok * float64(ctx))
	}
	return uint64(2 * m.Layers * m.KVHeads * m.HeadDim * ctx * kvBytesPerElt)
}

// OverheadBytes = 512 MiB + 5% of weights.
func OverheadBytes(weights uint64) uint64 {
	return overheadFixed + uint64(float64(weights)*overheadFrac)
}

// BudgetFor works out usable memory.
//   - Apple Silicon: macOS lets the GPU use about 2/3 of RAM (3/4 above 36 GB);
//     anything beyond that up to 85% of RAM can still run on the CPU.
//   - Discrete GPU: 95% of VRAM, plus 70% of system RAM for offload.
//   - CPU only: 70% of system RAM.
func BudgetFor(h hw.Info) Budget {
	ram := float64(h.RAMBytes)
	switch {
	case h.UnifiedMemory:
		frac := 2.0 / 3.0
		if h.RAMBytes > 36*GiB {
			frac = 0.75
		}
		return Budget{GPUBytes: uint64(ram * frac), TotalBytes: uint64(ram * 0.85), Kind: "unified"}
	case h.GPUMemBytes > 0:
		gpu := float64(h.GPUMemBytes) * 0.95
		return Budget{GPUBytes: uint64(gpu), TotalBytes: uint64(gpu + ram*0.7), Kind: "discrete"}
	default:
		return Budget{TotalBytes: uint64(ram * 0.7), Kind: "cpu"}
	}
}

// Estimate computes one quantization at a context length.
func Estimate(m model.Model, q Quant, ctx int, b Budget) Result {
	w := WeightsBytes(m.ParamsB, q)
	kv := KVCacheBytes(m, ctx)
	oh := OverheadBytes(w)
	r := Result{Quant: q.Name, WeightsBytes: w, KVCacheBytes: kv, OverheadBytes: oh, TotalBytes: w + kv + oh}
	switch {
	case b.GPUBytes > 0 && r.TotalBytes <= b.GPUBytes:
		r.Verdict, r.GPUFraction = Fits, 1
	case r.TotalBytes <= b.TotalBytes:
		r.Verdict = Partial
		if b.GPUBytes > 0 {
			// Floor, not round: a partial result must never read as 100% on GPU.
			r.GPUFraction = math.Floor(float64(b.GPUBytes)/float64(r.TotalBytes)*100) / 100
		}
	default:
		r.Verdict = No
	}
	return r
}

// All estimates every quantization, best quality first.
func All(m model.Model, ctx int, b Budget) []Result {
	out := make([]Result, 0, len(Quants))
	for _, q := range Quants {
		out = append(out, Estimate(m, q, ctx, b))
	}
	return out
}

// Best picks the highest-quality quant that fits fully; else the best partial;
// ok is false when nothing runs.
func Best(rs []Result) (Result, bool) {
	for _, want := range []string{Fits, Partial} {
		for _, r := range rs {
			if r.Verdict == want {
				return r, true
			}
		}
	}
	return Result{}, false
}
