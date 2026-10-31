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
