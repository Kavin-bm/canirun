package hw

import (
	"encoding/json"
	"strings"
)

// One PowerShell call gathers everything. Win32_VideoController.AdapterRAM is
// a uint32 and caps at 4 GB, so real VRAM comes from the display driver's
// registry key (HardwareInformation.qwMemorySize), which is 64-bit.
const windowsProbe = `$ErrorActionPreference='SilentlyContinue'
$cs=Get-CimInstance Win32_ComputerSystem
$p=Get-CimInstance Win32_Processor | Select-Object -First 1
$v=@(Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name })
$r=@(Get-ItemProperty 'HKLM:\SYSTEM\ControlSet001\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}\0*' | Where-Object { $_.'HardwareInformation.qwMemorySize' } | ForEach-Object {
  $m=$_.'HardwareInformation.qwMemorySize'; if ($m -is [byte[]]) { $m=[BitConverter]::ToUInt64($m,0) }
  [pscustomobject]@{Name=$_.DriverDesc; Mem=[uint64]$m} })
[pscustomobject]@{RAM=[uint64]$cs.TotalPhysicalMemory; CPU=$p.Name; Video=$v; Reg=$r} | ConvertTo-Json -Depth 3 -Compress`

func detectWindows(info *Info) {
	if out := run("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits"); out != "" {
		if name, mem, ok := ParseNvidiaSMI(out); ok {
			info.GPU, info.GPUMemBytes = name, mem
		}
	}
	out := run("powershell", "-NoProfile", "-NonInteractive", "-Command", windowsProbe)
	if out == "" {
		info.Notes = append(info.Notes, "PowerShell hardware query failed; RAM and GPU may be missing")
		return
	}
	ParseWindowsProbe([]byte(out), info)
}

// ParseWindowsProbe reads the JSON printed by windowsProbe. NVIDIA data from
// nvidia-smi, if already set, wins over the registry.
func ParseWindowsProbe(data []byte, info *Info) {
	var doc struct {
		RAM   uint64          `json:"RAM"`
		CPU   string          `json:"CPU"`
		Video json.RawMessage `json:"Video"`
		Reg   json.RawMessage `json:"Reg"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return
	}
	info.RAMBytes = doc.RAM
	info.CPU = strings.TrimSpace(doc.CPU)
	if info.GPUMemBytes > 0 {
		return
	}
	type adapter struct {
		Name string `json:"Name"`
		Mem  uint64 `json:"Mem"`
	}
	var regs []adapter
	if !unmarshalOneOrMany(doc.Reg, &regs) {
		var one adapter
		if json.Unmarshal(doc.Reg, &one) == nil && one.Name != "" {
			regs = []adapter{one}
		}
	}
	for _, a := range regs {
		if a.Mem > info.GPUMemBytes {
			info.GPU, info.GPUMemBytes = strings.TrimSpace(a.Name), a.Mem
		}
	}
	if info.GPU == "" {
		var names []string
		if !unmarshalOneOrMany(doc.Video, &names) {
			var one string
			if json.Unmarshal(doc.Video, &one) == nil {
				names = []string{one}
			}
		}
		if len(names) > 0 {
			info.GPU = names[0]
		}
	}
}

// PowerShell's ConvertTo-Json emits a bare value for one-element arrays.
func unmarshalOneOrMany(raw json.RawMessage, v any) bool {
	return len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, v) == nil
}
