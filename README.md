# canirun: stop downloading 20 GB models that won't run. Know in one second.

> One command tells you whether an LLM fits on *your* machine, at which quantization, before you download anything. Real hardware detection, honest math, clearly labelled ESTIMATE until measured.

<!-- TODO: demo GIF of `canirun llama3.1:8b` on a MacBook goes here -->
**TODO: demo GIF / result card screenshot**

## Quick start

```sh
go run github.com/kavinbm/canirun/cmd/canirun@latest llama3.1:8b
```

> TODO: the module path `github.com/kavinbm/canirun` is a placeholder until the GitHub repo exists. Homebrew and prebuilt binaries are coming soon.

Or from a clone: `make build && ./bin/canirun llama3.1:8b`

## What it does today

| Command | What it does |
|---|---|
| `canirun <model>` | ESTIMATE of memory for F16, Q8_0, Q5_K_M and Q4_K_M: weights + KV cache + runtime overhead, with a verdict: **fits** (all in GPU/unified memory), **partial** (CPU offload or CPU only, slower) or **no**. Options: `--ctx`, `--quant`, `--json`. |
| `canirun 13b` | Same, for any model size not in the catalog (KV cache is a rough guess). |
| `canirun hw` | Detected OS, CPU, cores, RAM, GPU / Apple Silicon chip, unified memory or VRAM (macOS and Linux; NVIDIA via `nvidia-smi`, AMD VRAM via sysfs). |
| `canirun list` | The built-in catalog in [`models/`](models/). |
| `canirun recommend` | Catalog models that run here, with the best quantization for each. |

Example (from a CPU-only Linux box):

```text
QUANT   WEIGHTS  KV CACHE  OVERHEAD  TOTAL    VERDICT
F16     15.0 GB  1.0 GB    1.2 GB    17.2 GB  ❌ no
Q8_0    7.9 GB   1.0 GB    0.9 GB    9.8 GB   🟡 partial (CPU only, slow)
Q4_K_M  4.5 GB   1.0 GB    0.7 GB    6.3 GB   🟡 partial (CPU only, slow)
```

### How the estimate works

- **Weights** = parameters × effective bits per weight ÷ 8 (F16 16, Q8_0 8.5, Q5_K_M 5.69, Q4_K_M 4.85).
- **KV cache** = 2 × layers × KV heads × head dim × context × 2 bytes (F16 cache).
- **Overhead** = 512 MB + 5% of weights.
- **Budget:** Apple Silicon GPU gets ~2/3 of RAM (3/4 above 36 GB); discrete GPUs get 95% of VRAM plus 70% of RAM for offload; CPU-only gets 70% of RAM.

### Coming soon (not built yet)

- Speed estimate (tokens/sec) from memory bandwidth
- `canirun bench`: real measured speed via Ollama or llama.cpp, shown next to the estimate
- Any Hugging Face model by repo name, shareable result card
- `canirun mcp`: MCP server for coding agents
- Windows support, release binaries

## Promises

- **Honest labels.** Every number is marked ESTIMATE until it's measured on real hardware.
- **Nothing downloaded, nothing uploaded.** No telemetry; hardware info stays on your machine.
- **Read-only.** It only reads system info (`sysctl`, `system_profiler`, `/proc`, `nvidia-smi`).
- **Single binary, zero dependencies.**

## Paste this into your coding agent

```text
Run canirun (https://github.com/kavinbm/canirun) to check which local LLMs my machine can run:
`go run github.com/kavinbm/canirun/cmd/canirun@latest recommend --json`, then
`canirun <model> --json` for any model I ask about. Explain the verdicts in plain words,
say clearly that these are estimates, and don't download any model without asking me.
```

## Add a model or your benchmark (we need your help!)

The catalog is one small YAML file per model in [`models/`](models/). Adding a model takes two minutes: copy a file, fill in five numbers from the model's `config.json`, open a PR. Soon you'll also be able to submit measured results from your machine. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT, see [LICENSE](LICENSE).
