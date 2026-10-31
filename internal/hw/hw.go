// Package hw detects the hardware that matters for running local LLMs.
package hw

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Info is the detected hardware. Byte sizes are 0 when unknown.
type Info struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	CPU           string `json:"cpu,omitempty"`
	Cores         int    `json:"cores"`
	PerfCores     int    `json:"performance_cores,omitempty"`
	EffCores      int    `json:"efficiency_cores,omitempty"`
	RAMBytes      uint64 `json:"ram_bytes"`
	GPU           string `json:"gpu,omitempty"`
	GPUCores      int    `json:"gpu_cores,omitempty"`
	GPUMemBytes   uint64 `json:"gpu_memory_bytes,omitempty"`
	UnifiedMemory bool   `json:"unified_memory"`
	// GPUWiredLimitBytes is a user-raised Apple GPU memory limit
	// (sysctl iogpu.wired_limit_mb); 0 means the macOS default.
	GPUWiredLimitBytes uint64 `json:"gpu_wired_limit_bytes,omitempty"`
	// Memory bandwidth drives the speed estimate. Source is "table" (known
	// chip), "assumed" (typical value, chip unknown) or "override" (--bw).
	GPUBandwidthGBps float64  `json:"gpu_bandwidth_gbps,omitempty"`
	CPUBandwidthGBps float64  `json:"cpu_bandwidth_gbps"`
	BandwidthSource  string   `json:"bandwidth_source"`
	Notes            []string `json:"notes,omitempty"`
}

// minDiscreteVRAM filters out iGPU carve-outs (often 128-512 MB) that
// would otherwise be treated as a discrete GPU.
const minDiscreteVRAM = 2 << 30

// Detect inspects the current machine. It never fails hard: anything it
// can't read is left empty and noted.
func Detect() Info {
	info := Info{OS: runtime.GOOS, Arch: runtime.GOARCH, Cores: runtime.NumCPU()}
	switch runtime.GOOS {
	case "darwin":
		detectDarwin(&info)
	case "linux":
		detectLinux(&info)
	case "windows":
		detectWindows(&info)
	default:
		info.Notes = append(info.Notes, runtime.GOOS+" detection is coming soon; only OS, arch and core count are shown")
	}
	Finish(&info)
	return info
}

// Finish applies the rules shared by every OS: drop iGPU carve-outs and
// fill in memory bandwidth. Exported so callers can re-run it after
// overriding fields (for "what if I had 24 GB VRAM?").
func Finish(info *Info) {
	if !info.UnifiedMemory && info.GPUMemBytes > 0 && info.GPUMemBytes < minDiscreteVRAM {
		info.Notes = append(info.Notes, "GPU has under 2 GB of dedicated memory; treating this machine as CPU only")
		info.GPUMemBytes = 0
	}
	if info.BandwidthSource == "override" {
		return
	}
	gpuBW, cpuBW, known := Bandwidth(*info)
	info.GPUBandwidthGBps, info.CPUBandwidthGBps = gpuBW, cpuBW
	info.BandwidthSource = "assumed"
	if known {
		info.BandwidthSource = "table"
	}
}

// cmdTimeout bounds every external probe: a wedged GPU driver can make
// nvidia-smi hang forever, and detection must never hang the CLI.
const cmdTimeout = 5 * time.Second

func run(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sysctl(key string) string { return run("sysctl", "-n", key) }

func detectDarwin(info *Info) {
	info.CPU = sysctl("machdep.cpu.brand_string")
	if n, err := strconv.Atoi(sysctl("hw.ncpu")); err == nil {
		info.Cores = n
	}
	info.PerfCores, _ = strconv.Atoi(sysctl("hw.perflevel0.physicalcpu"))
	info.EffCores, _ = strconv.Atoi(sysctl("hw.perflevel1.physicalcpu"))
	info.RAMBytes, _ = strconv.ParseUint(sysctl("hw.memsize"), 10, 64)
	if mb, _ := strconv.ParseUint(sysctl("iogpu.wired_limit_mb"), 10, 64); mb > 0 {
		info.GPUWiredLimitBytes = mb << 20
	}
	if out := run("system_profiler", "SPDisplaysDataType", "-json"); out != "" {
		ParseSPDisplays([]byte(out), info)
	}
	if runtime.GOARCH == "arm64" {
		info.UnifiedMemory = true
		info.GPUMemBytes = info.RAMBytes
		if info.GPU == "" {
			info.GPU = info.CPU
		}
	}
}

// ParseSPDisplays reads `system_profiler SPDisplaysDataType -json`.
func ParseSPDisplays(data []byte, info *Info) {
	var doc struct {
		Displays []map[string]any `json:"SPDisplaysDataType"`
	}
	if json.Unmarshal(data, &doc) != nil || len(doc.Displays) == 0 {
		return
	}
	d := doc.Displays[0]
	if s, ok := d["sppci_model"].(string); ok {
		info.GPU = s
	}
	if s, ok := d["sppci_cores"].(string); ok {
		info.GPUCores, _ = strconv.Atoi(s)
	}
	// Only dedicated VRAM counts. "spdisplays_vram_shared" (Intel iGPU) is
	// carved out of system RAM and must not be treated as a discrete GPU.
	if s, ok := d["spdisplays_vram"].(string); ok {
		info.GPUMemBytes = ParseSize(s)
	}
}

// ParseSize parses "8 GB", "1536 MB" into bytes (binary units).
func ParseSize(s string) uint64 {
	f := strings.Fields(s)
	if len(f) != 2 {
		return 0
	}
	n, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(f[1]) {
	case "GB":
		return uint64(n * (1 << 30))
	case "MB":
		return uint64(n * (1 << 20))
	}
	return 0
}

func detectLinux(info *Info) {
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		info.RAMBytes = ParseMeminfo(string(data))
	}
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		info.CPU = ParseCPUInfoModel(string(data))
	}
	if info.CPU == "" { // ARM /proc/cpuinfo has no "model name"
		if n := ParseCPUInfoModel(strings.ReplaceAll(run("lscpu"), "Model name", "model name")); n != "" && n != "-" {
			info.CPU = n
		} else {
			info.CPU = runtime.GOARCH + " CPU"
		}
	}
	if out := run("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits"); out != "" {
		if name, mem, ok := ParseNvidiaSMI(out); ok {
			info.GPU, info.GPUMemBytes = name, mem
			return
		}
	}
	// AMD (amdgpu) exposes VRAM size in sysfs. Take the largest card: card0
	// is often a small integrated GPU.
	files, _ := filepath.Glob("/sys/class/drm/card*/device/mem_info_vram_total")
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			if n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); n > info.GPUMemBytes {
				info.GPUMemBytes = n
			}
		}
	}
	if out := run("lspci"); out != "" {
		info.GPU = ParseLspciGPU(out)
	}
	if info.GPU == "" && info.GPUMemBytes > 0 {
		info.GPU = "AMD GPU"
	}
	if info.GPU == "" {
		info.Notes = append(info.Notes, "no GPU detected; estimates assume CPU only")
	}
}

// ParseMeminfo returns MemTotal in bytes.
func ParseMeminfo(data string) uint64 {
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && f[0] == "MemTotal:" {
			kb, _ := strconv.ParseUint(f[1], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

// ParseCPUInfoModel returns the first "model name" in /proc/cpuinfo.
func ParseCPUInfoModel(data string) string {
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ParseNvidiaSMI parses
// `nvidia-smi --query-gpu=name,memory.total --format=csv,noheader,nounits`.
// With several GPUs, VRAM is summed (llama.cpp and Ollama split layers
// across cards) and the name becomes "2x NVIDIA GeForce RTX 3090".
func ParseNvidiaSMI(out string) (name string, mem uint64, ok bool) {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		n, memStr, found := strings.Cut(line, ",")
		if !found {
			continue
		}
		mib, err := strconv.ParseUint(strings.TrimSpace(memStr), 10, 64)
		if err != nil {
			continue
		}
		names = append(names, strings.TrimSpace(n))
		mem += mib << 20
	}
	if len(names) == 0 {
		return "", 0, false
	}
	name = names[0]
	if len(names) > 1 {
		same := true
		for _, n := range names {
			same = same && n == names[0]
		}
		if same {
			name = fmt.Sprintf("%dx %s", len(names), names[0])
		} else {
			name = strings.Join(names, " + ")
		}
	}
	return name, mem, true
}

// ParseLspciGPU returns the VGA/3D/display controller description,
// preferring a discrete (non-Intel) GPU over an integrated one.
func ParseLspciGPU(out string) string {
	first := ""
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(line)
		if !strings.Contains(l, "vga compatible controller") && !strings.Contains(l, "3d controller") && !strings.Contains(l, "display controller") {
			continue
		}
		_, v, ok := strings.Cut(line, "controller: ")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if !strings.HasPrefix(strings.ToLower(v), "intel") {
			return v
		}
		if first == "" {
			first = v
		}
	}
	return first
}

// GiB formats bytes as GiB with one decimal.
func GiB(b uint64) string { return strconv.FormatFloat(float64(b)/(1<<30), 'f', 1, 64) + " GB" }
