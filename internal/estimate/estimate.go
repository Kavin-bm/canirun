// Package estimate does the memory math: weights + KV cache + overhead,
// compared against what the machine can give the model.
//
// All results are ESTIMATES. Measured numbers come from `canirun bench`.
package estimate

import (
	"math"

	"github.com/kavinbm16/canirun/internal/hw"
	"github.com/kavinbm16/canirun/internal/model"
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
	// TokensPerSec is the ESTIMATED generation speed at short context (0 if it won't run).
	TokensPerSec float64 `json:"est_tokens_per_sec"`
}

// Budget is how much memory the machine can give a model.
type Budget struct {
	GPUBytes   uint64 `json:"gpu_bytes"`   // fast memory (VRAM or GPU share of unified memory)
	TotalBytes uint64 `json:"total_bytes"` // GPU + usable system RAM
	Kind       string `json:"kind"`        // unified, discrete, cpu
	// Memory bandwidth in GB/s, copied from hw.Info, for the speed estimate.
	GPUBandwidthGBps float64 `json:"gpu_bandwidth_gbps"`
	CPUBandwidthGBps float64 `json:"cpu_bandwidth_gbps"`
}

// Efficiency is the share of peak memory bandwidth llama.cpp-style runtimes
// reach while generating. Calibrated from measured runs of Llama 3.2 3B
// Q4_K_M on an M4 Pro (273 GB/s): Ollama 89-93 tok/s, llama-bench 94 tok/s,
// against a 140 tok/s theoretical ceiling = 0.66.
// CPUEfficiency is an assumption until community results calibrate it.
// These are the calibration knobs: change them when measured data says so.
var (
	GPUEfficiency = 0.66
	CPUEfficiency = 0.6
)

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
//     A raised `iogpu.wired_limit_mb` replaces the default share.
func BudgetFor(h hw.Info) Budget {
	ram := float64(h.RAMBytes)
	var b Budget
	switch {
	case h.UnifiedMemory:
		frac := 2.0 / 3.0
		if h.RAMBytes > 36*GiB {
			frac = 0.75
		}
		b = Budget{GPUBytes: uint64(ram * frac), TotalBytes: uint64(ram * 0.85), Kind: "unified"}
		if h.GPUWiredLimitBytes > 0 && h.GPUWiredLimitBytes <= h.RAMBytes {
			b.GPUBytes = h.GPUWiredLimitBytes
			b.TotalBytes = max(b.TotalBytes, b.GPUBytes)
		}
	case h.GPUMemBytes > 0:
		gpu := float64(h.GPUMemBytes) * 0.95
		b = Budget{GPUBytes: uint64(gpu), TotalBytes: uint64(gpu + ram*0.7), Kind: "discrete"}
	default:
		b = Budget{TotalBytes: uint64(ram * 0.7), Kind: "cpu"}
	}
	b.GPUBandwidthGBps, b.CPUBandwidthGBps = h.GPUBandwidthGBps, h.CPUBandwidthGBps
	return b
}

// TokensPerSec estimates generation speed. Each token reads every active
// weight once, so time per token = bytes on GPU / GPU bandwidth + bytes on
// CPU / CPU bandwidth, each scaled by measured efficiency. KV-cache reads are
// ignored, so this is the speed at short context; long chats are slower.
func TokensPerSec(m model.Model, q Quant, gpuFraction float64, b Budget) float64 {
	bytes := float64(WeightsBytes(m.ActiveB(), q))
	if bytes == 0 {
		return 0
	}
	var sec float64
	if gpuFraction > 0 {
		if b.GPUBandwidthGBps <= 0 {
			return 0
		}
		sec += bytes * gpuFraction / (b.GPUBandwidthGBps * 1e9 * GPUEfficiency)
	}
	if gpuFraction < 1 {
		if b.CPUBandwidthGBps <= 0 {
			return 0
		}
		sec += bytes * (1 - gpuFraction) / (b.CPUBandwidthGBps * 1e9 * CPUEfficiency)
	}
	return math.Round(10/sec) / 10
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
	if r.Verdict != No {
		r.TokensPerSec = TokensPerSec(m, q, r.GPUFraction, b)
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

// Best picks the highest-quality quant that fits fully in GPU memory, but
// skips F16 when Q8_0 fits: Q8_0 is near-lossless at twice the speed. If
// nothing fits, it picks the highest-quality partial (offload or CPU-only)
// quant that still reaches a comfortable 10 tok/s, else the fastest partial.
// ok is false when nothing runs.
func Best(rs []Result) (Result, bool) {
	var fastest *Result
	for i, r := range rs {
		if r.Verdict == Fits && (r.Quant != "F16" || i == len(rs)-1 || rs[i+1].Verdict != Fits) {
			return r, true
		}
		if r.Verdict == Partial && (fastest == nil || r.TokensPerSec > fastest.TokensPerSec) {
			fastest = &rs[i]
		}
	}
	for _, r := range rs {
		if r.Verdict == Partial && r.TokensPerSec >= 10 {
			return r, true
		}
	}
	if fastest != nil {
		return *fastest, true
	}
	return Result{}, false
}
