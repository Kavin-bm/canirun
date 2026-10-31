package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kavinbm/canirun/internal/hw"
)

const gib = 1 << 30

func fakeHW(t *testing.T, h hw.Info) {
	t.Helper()
	old := detect
	detect = func() hw.Info { return h }
	t.Cleanup(func() { detect = old })
}

func runCLI(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestExitCodes(t *testing.T) {
	fakeHW(t, hw.Info{OS: "darwin", Arch: "arm64", RAMBytes: 16 * gib, UnifiedMemory: true, GPU: "Apple M2"})
	cases := []struct {
		args []string
		want int
	}{
		{nil, exitUsage},
		{[]string{"llama3.1:8b"}, exitOK},
		{[]string{"llama3.3:70b"}, exitNoFit},
		{[]string{"not-a-model"}, exitUsage},
		{[]string{"llama3.1:8b", "--ctx", "-1"}, exitUsage},
		{[]string{"llama3.1:8b", "--bogus"}, exitUsage},
		{[]string{"llama3.1:8b", "--quant", "Q3"}, exitUsage},
		{[]string{"hw", "extra"}, exitUsage},
		{[]string{"recommend", "--json"}, exitOK},
		{[]string{"version"}, exitOK},
	}
	for _, c := range cases {
		if got, _, _ := runCLI(c.args...); got != c.want {
			t.Errorf("canirun %v: exit %d, want %d", c.args, got, c.want)
		}
	}
}

func TestJSONErrorsAreJSON(t *testing.T) {
	fakeHW(t, hw.Info{RAMBytes: 16 * gib})
	code, out, _ := runCLI("not-a-model", "--json")
	var v struct {
		Error    string `json:"error"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.ExitCode != code || v.Error == "" {
		t.Fatalf("not a JSON error: %q (%v)", out, err)
	}
}

func TestUnknownRAMIsAnErrorNotANo(t *testing.T) {
	fakeHW(t, hw.Info{OS: "plan9"})
	code, _, errOut := runCLI("llama3.2:3b")
	if code != exitError || !strings.Contains(errOut, "RAM") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestEstimateJSON(t *testing.T) {
	fakeHW(t, hw.Info{RAMBytes: 32 * gib, GPUMemBytes: 8 * gib, GPU: "RTX 3070"})
	code, out, _ := runCLI("llama3.1:8b", "--json", "--quant", "q4_k_m")
	var v estimateOutput
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if code != exitOK || v.Label != "ESTIMATE" || len(v.Results) != 1 || v.Verdict != "fits" {
		t.Fatalf("code %d, %+v", code, v)
	}
}
