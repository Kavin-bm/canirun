package hw

import "testing"

func TestParsers(t *testing.T) {
	if got := ParseMeminfo("MemTotal:       16303428 kB\nMemFree: 1 kB\n"); got != 16303428*1024 {
		t.Fatalf("meminfo %d", got)
	}
	if got := ParseCPUInfoModel("processor\t: 0\nmodel name\t: AMD Ryzen 9 7950X 16-Core Processor\n"); got != "AMD Ryzen 9 7950X 16-Core Processor" {
		t.Fatalf("cpuinfo %q", got)
	}
	name, mem, ok := ParseNvidiaSMI("NVIDIA GeForce RTX 4090, 24564\n")
	if !ok || name != "NVIDIA GeForce RTX 4090" || mem != 24564<<20 {
		t.Fatalf("nvidia %q %d %v", name, mem, ok)
	}
	name, mem, ok = ParseNvidiaSMI("NVIDIA GeForce RTX 3090, 24576\nNVIDIA GeForce RTX 3090, 24576\n")
	if !ok || name != "2x NVIDIA GeForce RTX 3090" || mem != 2*24576<<20 {
		t.Fatalf("multi nvidia %q %d %v", name, mem, ok)
	}
	if got := ParseLspciGPU("00:02.0 VGA compatible controller: Intel Corporation UHD Graphics 620 (rev 07)"); got != "Intel Corporation UHD Graphics 620 (rev 07)" {
		t.Fatalf("lspci %q", got)
	}
	if ParseSize("8 GB") != 8<<30 || ParseSize("1536 MB") != 1536<<20 {
		t.Fatal("size")
	}
}

func TestParseSPDisplays(t *testing.T) {
	var info Info
	ParseSPDisplays([]byte(`{"SPDisplaysDataType":[{"sppci_model":"Apple M4 Pro","sppci_cores":"20"}]}`), &info)
	if info.GPU != "Apple M4 Pro" || info.GPUCores != 20 {
		t.Fatalf("%+v", info)
	}
}

func TestParseSPDisplaysIgnoresSharedVRAM(t *testing.T) {
	var info Info
	ParseSPDisplays([]byte(`{"SPDisplaysDataType":[{"sppci_model":"Intel Iris Plus","spdisplays_vram_shared":"1536 MB"}]}`), &info)
	if info.GPUMemBytes != 0 {
		t.Fatalf("shared iGPU memory counted as VRAM: %+v", info)
	}
}

func TestLookupBandwidth(t *testing.T) {
	cases := []struct {
		name  string
		cores int
		want  float64
	}{
		{"Apple M4 Pro", 16, 273},
		{"Apple M4", 10, 120},
		{"Apple M3 Max", 30, 300},
		{"Apple M3 Max", 40, 400},
		{"NVIDIA GeForce RTX 4070 Ti SUPER", 0, 672},
		{"NVIDIA GeForce RTX 4070", 0, 504},
		{"2x NVIDIA GeForce RTX 3090", 0, 936},
		{"NVIDIA GeForce RTX 4090 Laptop GPU", 0, 576},
		{"AMD Radeon RX 7900 XTX", 0, 960},
		{"NVIDIA A100-SXM4-80GB", 0, 2039},
	}
	for _, c := range cases {
		if got, ok := LookupBandwidth(c.name, c.cores); !ok || got != c.want {
			t.Errorf("%s: %v %v, want %v", c.name, got, ok, c.want)
		}
	}
	if _, ok := LookupBandwidth("Mystery GPU 9000", 0); ok {
		t.Error("unknown GPU must not match")
	}
}

func TestFinishDropsTinyVRAMAndSetsBandwidth(t *testing.T) {
	info := Info{RAMBytes: 16 << 30, GPU: "AMD Radeon Graphics", GPUMemBytes: 512 << 20}
	Finish(&info)
	if info.GPUMemBytes != 0 || info.BandwidthSource != "assumed" || info.CPUBandwidthGBps == 0 {
		t.Fatalf("%+v", info)
	}
}

func TestParseWindowsProbe(t *testing.T) {
	var info Info
	ParseWindowsProbe([]byte(`{"RAM":34359738368,"CPU":"AMD Ryzen 7 7800X3D 8-Core Processor ","Video":"AMD Radeon RX 7900 XTX","Reg":{"Name":"AMD Radeon RX 7900 XTX","Mem":25753026560}}`), &info)
	if info.RAMBytes != 32<<30 || info.GPU != "AMD Radeon RX 7900 XTX" || info.GPUMemBytes != 25753026560 || info.CPU != "AMD Ryzen 7 7800X3D 8-Core Processor" {
		t.Fatalf("%+v", info)
	}
	var multi Info
	ParseWindowsProbe([]byte(`{"RAM":17179869184,"CPU":"Intel","Video":["Intel UHD","NVIDIA GeForce RTX 3060"],"Reg":[{"Name":"Intel UHD","Mem":134217728},{"Name":"NVIDIA GeForce RTX 3060","Mem":12884901888}]}`), &multi)
	if multi.GPU != "NVIDIA GeForce RTX 3060" || multi.GPUMemBytes != 12<<30 {
		t.Fatalf("%+v", multi)
	}
}
