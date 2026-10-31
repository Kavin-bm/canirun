# Good first issues

Copy any of these into a GitHub issue (label: `good first issue`).

### 1. Add your benchmark result
Run `canirun bench <model> --save` on your machine and PR the file into `models/results/`.
**Done when:** `canirun validate models/results/<file>.yaml` passes and CI is green.

### 2. Add a missing GPU to the bandwidth table
`canirun hw` prints "ASSUMED" for GPUs missing from `internal/hw/bandwidth.go` (e.g. RTX 2060 Super, RX 6600, Arc A770, Jetson Orin). Add the spec-sheet bandwidth and a case in `TestLookupBandwidth`.

### 3. Add a model to the catalog
Candidates: Gemma 3n E4B, Llama 3.2 Vision 11B, Command R 35B, Phi-4 Reasoning, OLMo 2, Granite 4. Copy a file in `models/`, fill in `config.json` numbers, run `canirun validate`.

### 4. Shell completions
Generate bash/zsh/fish completions for subcommands and flags (stdlib only), and wire them into the GoReleaser Homebrew cask.

### 5. Test Windows detection on real hardware
Run `canirun hw --json` on Windows with an NVIDIA, AMD or Intel Arc GPU and report whether RAM, GPU name and VRAM are right. Fix `ParseWindowsProbe` if not, with the real output as a test case.

### 6. Show a "download size" column
For each quant, show the approximate GGUF download size (weights only) next to "needs", in the CLI and the UI.

### 7. `canirun compare a b`
Print two models side by side (memory, speed, verdict) for the same machine.

### 8. Light-theme share card
The PNG share card is always dark. Add a light variant that follows the UI theme.
