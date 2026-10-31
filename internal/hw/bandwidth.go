package hw

import (
	"sort"
	"strings"
)

// Peak memory bandwidth in GB/s from vendor spec sheets. Token generation is
// memory-bound, so this is the main input to the speed estimate.
// Add your GPU by pull request.
var gpuBandwidth = map[string]float64{
	// Apple Silicon (unified memory). M3 Max / M4 Max differ by GPU cores; see appleBandwidth.
	"apple m1": 68, "apple m1 pro": 200, "apple m1 max": 400, "apple m1 ultra": 800,
	"apple m2": 100, "apple m2 pro": 200, "apple m2 max": 400, "apple m2 ultra": 800,
	"apple m3": 100, "apple m3 pro": 150, "apple m3 max": 400, "apple m3 ultra": 819,
	"apple m4": 120, "apple m4 pro": 273, "apple m4 max": 546,
	"apple m5": 153,

	// NVIDIA consumer
	"rtx 5090": 1792, "rtx 5080": 960, "rtx 5070 ti": 896, "rtx 5070": 672, "rtx 5060 ti": 448, "rtx 5060": 448,
	"rtx 4090": 1008, "rtx 4080 super": 736, "rtx 4080": 717, "rtx 4070 ti super": 672, "rtx 4070 ti": 504,
	"rtx 4070 super": 504, "rtx 4070": 504, "rtx 4060 ti": 288, "rtx 4060": 272,
	"rtx 3090 ti": 1008, "rtx 3090": 936, "rtx 3080 ti": 912, "rtx 3080": 760, "rtx 3070 ti": 608,
	"rtx 3070": 448, "rtx 3060 ti": 448, "rtx 3060": 360,
	"rtx 2080 ti": 616, "rtx 2080": 448, "rtx 2070": 448, "rtx 2060": 336, "titan rtx": 672,
	"gtx 1080 ti": 484, "gtx 1080": 320, "gtx 1070": 256,
	// NVIDIA laptop parts are much slower than the desktop card with the same name.
	"rtx 4090 laptop": 576, "rtx 4080 laptop": 432, "rtx 4070 laptop": 256, "rtx 4060 laptop": 256,
	"rtx 3080 laptop": 448, "rtx 3070 laptop": 448, "rtx 3060 laptop": 336,
	// NVIDIA workstation / datacenter
	"rtx 6000 ada": 960, "rtx a6000": 768, "rtx a5000": 768, "rtx a4000": 448,
	"a100": 1555, "a100 80gb pcie": 1935, "a100-sxm4-80gb": 2039, "h100 pcie": 2000, "h100": 3350, "h200": 4800,
	"l40s": 864, "l40": 864, "l4": 300, "t4": 320, "a10g": 600, "a10": 600, "v100": 900,
	// AMD
	"rx 9070 xt": 640, "rx 9070": 640, "rx 7900 xtx": 960, "rx 7900 xt": 800, "rx 7900 gre": 576,
	"rx 7800 xt": 624, "rx 7700 xt": 432, "rx 7600": 288, "rx 6950 xt": 576, "rx 6900 xt": 512,
	"rx 6800 xt": 512, "rx 6800": 512, "rx 6700 xt": 384, "radeon vii": 1024, "mi300x": 5300, "mi250": 3200,
	"radeon 8060s": 256,
}

// Typical values when the chip is unknown. Deliberately conservative.
const (
	assumedGPUBandwidth = 300 // mid-range discrete GPU
	assumedCPUBandwidth = 50  // dual-channel DDR4/DDR5 desktop, as llama.cpp sees it
)

// keysLongestFirst makes "rtx 4070 ti super" win over "rtx 4070".
var keysLongestFirst = func() []string {
	keys := make([]string, 0, len(gpuBandwidth))
	for k := range gpuBandwidth {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}()

// LookupBandwidth finds a GPU or Apple chip by name; ok is false if unknown.
func LookupBandwidth(name string, gpuCores int) (float64, bool) {
	n := " " + strings.ToLower(name) + " "
	n = strings.NewReplacer("geforce ", "", "nvidia ", "", "amd ", "", "radeon rx", "rx", "  ", " ").Replace(n)
	if strings.Contains(n, "laptop") || strings.Contains(n, "mobile") {
		n = strings.Replace(n, " gpu", "", 1)
	}
	for _, k := range keysLongestFirst {
		if !strings.Contains(n, " "+k+" ") && !strings.Contains(n, " "+k+"-") && !strings.Contains(n, " "+k+" laptop") {
			continue
		}
		if strings.Contains(n, "laptop") || strings.Contains(n, "mobile") {
			if v, ok := gpuBandwidth[k+" laptop"]; ok {
				return v, true
			}
		}
		return appleBandwidth(k, gpuCores), true
	}
	return 0, false
}

// appleBandwidth handles Max chips whose bandwidth depends on GPU core count.
func appleBandwidth(key string, gpuCores int) float64 {
	switch {
	case key == "apple m3 max" && gpuCores > 0 && gpuCores <= 30:
		return 300
	case key == "apple m4 max" && gpuCores > 0 && gpuCores <= 32:
		return 410
	}
	return gpuBandwidth[key]
}

// Bandwidth returns GPU and CPU memory bandwidth for the speed estimate,
// and whether the GPU (or Apple chip) was found in the table.
func Bandwidth(h Info) (gpu, cpu float64, known bool) {
	switch {
	case h.UnifiedMemory:
		gpu, known = LookupBandwidth(h.GPU, h.GPUCores)
		if !known {
			gpu, known = LookupBandwidth(h.CPU, h.GPUCores)
		}
		if !known {
			gpu = 100
		}
		// CPU cores cannot saturate Apple's memory bus the way the GPU does.
		return gpu, gpu / 2, known
	case h.GPUMemBytes > 0:
		gpu, known = LookupBandwidth(h.GPU, h.GPUCores)
		if !known {
			gpu = assumedGPUBandwidth
		}
		return gpu, assumedCPUBandwidth, known
	}
	// CPU only: RAM speed is not detected, so this is always an assumption.
	return 0, assumedCPUBandwidth, false
}
