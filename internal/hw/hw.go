// Package hw detects the hardware that matters for running local LLMs.
package hw

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Info is the detected hardware. Byte sizes are 0 when unknown.
type Info struct {
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	CPU           string   `json:"cpu,omitempty"`
	Cores         int      `json:"cores"`
	PerfCores     int      `json:"performance_cores,omitempty"`
	EffCores      int      `json:"efficiency_cores,omitempty"`
	RAMBytes      uint64   `json:"ram_bytes"`
	GPU           string   `json:"gpu,omitempty"`
	GPUCores      int      `json:"gpu_cores,omitempty"`
	GPUMemBytes   uint64   `json:"gpu_memory_bytes,omitempty"`
	UnifiedMemory bool     `json:"unified_memory"`
	Notes         []string `json:"notes,omitempty"`
}

// Detect inspects the current machine. It never fails hard: anything it
// can't read is left empty and noted.
func Detect() Info {
	info := Info{OS: runtime.GOOS, Arch: runtime.GOARCH, Cores: runtime.NumCPU()}
	switch runtime.GOOS {
	case "darwin":
		detectDarwin(&info)
	case "linux":
		detectLinux(&info)
	default:
		info.Notes = append(info.Notes, runtime.GOOS+" detection is coming soon; only OS, arch and core count are shown")
	}
	return info
}

func run(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
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
	for _, k := range []string{"spdisplays_vram", "spdisplays_vram_shared"} {
		if s, ok := d[k].(string); ok {
			if b := ParseSize(s); b > 0 {
				info.GPUMemBytes = b
				break
			}
		}
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
	if out := run("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits"); out != "" {
		if name, mem, ok := ParseNvidiaSMI(out); ok {
			info.GPU, info.GPUMemBytes = name, mem
			return
		}
	}
	// AMD (amdgpu) exposes VRAM size in sysfs.
	if files, _ := filepath.Glob("/sys/class/drm/card*/device/mem_info_vram_total"); len(files) > 0 {
		if b, err := os.ReadFile(files[0]); err == nil {
			info.GPUMemBytes, _ = strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		}
	}
	if out := run("lspci"); out != "" {
		info.GPU = ParseLspciGPU(out)
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

// ParseNvidiaSMI parses the first line of
// `nvidia-smi --query-gpu=name,memory.total --format=csv,noheader,nounits`.
func ParseNvidiaSMI(out string) (name string, mem uint64, ok bool) {
	line := strings.SplitN(out, "\n", 2)[0]
	name, memStr, found := strings.Cut(line, ",")
	if !found {
		return "", 0, false
	}
	mib, err := strconv.ParseUint(strings.TrimSpace(memStr), 10, 64)
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(name), mib << 20, true
}

// ParseLspciGPU returns the first VGA/3D controller description.
func ParseLspciGPU(out string) string {
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "vga compatible controller") || strings.Contains(l, "3d controller") {
			if _, v, ok := strings.Cut(line, "controller: "); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// GiB formats bytes as GiB with one decimal.
func GiB(b uint64) string { return strconv.FormatFloat(float64(b)/(1<<30), 'f', 1, 64) + " GB" }
