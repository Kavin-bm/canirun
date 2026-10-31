# canirun: Plan

## How it works
1. **Detect hardware:** OS, CPU cores and instruction sets, RAM, GPU (NVIDIA via NVML or `nvidia-smi`, Apple Silicon unified memory, AMD basics), VRAM, memory bandwidth (from a lookup table by chip, or a quick measurement).
2. **Resolve the model:** name or Hugging Face repo to parameter count, layers, KV-cache size, context length, and available quantizations (GGUF sizes from the Hub API, cached locally).
3. **Estimate:**
   - Memory needed = weights (by quantization) + KV cache (by context) + runtime overhead.
   - Speed estimate for generation = memory bandwidth / bytes read per token, scaled by an efficiency factor from measured data. Prompt processing is estimated from compute.
   - Verdict: fits fully on GPU, partial offload, CPU only, or does not fit.
4. **Benchmark (opt-in):** detect Ollama or llama.cpp, run a short fixed prompt, record time to first token and tokens per second, and compare to the estimate.
5. **Report:** terminal output and a shareable card, with JSON output for agents.

## Commands
- `canirun <model>`: verdict and best quantization.
- `canirun recommend`: top models that fit, by task size.
- `canirun bench [model]`: real measured speed.
- `canirun hw`: show detected hardware.
- `canirun mcp`: expose the same functions to coding agents.

## Repo layout
```
canirun/
  cmd/canirun/
  internal/hw/          hardware detection per OS
  internal/model/       Hub metadata and catalog
  internal/estimate/    memory and speed math
  internal/bench/       Ollama and llama.cpp adapters
  internal/report/      terminal, JSON, card
  models/               community YAML overrides and measured results
  README.md, CONTRIBUTING.md, LICENSE (MIT)
```

## Milestones
- **M0 (day 1):** repo, license, `hw` command on macOS and Linux.
- **M1 (days 2-3):** model resolver and memory estimate, verdict output.
- **M2 (days 4-5):** speed estimate, calibrated against real benchmark numbers on Kavin's MacBook.
- **M3 (days 6-7):** Ollama benchmark adapter, result card, `recommend`.
- **M4 (week 2):** Windows and NVIDIA support, MCP server, release binaries, demo GIF.
- **Launch:** a short demo post on X with the result card, plus a Show HN. Invite people to submit benchmark results by PR.

## Risks
- Estimates go stale as runtimes change. Mitigate with measured results in the repo and a visible "estimated vs measured" label.
- Existing sites (such as llmspeed.dev) are web-only, so the CLI's edge is real hardware detection and real measurement. Compare carefully before launch.
- Cross-platform GPU detection takes time. Launch with Mac and NVIDIA first.

## Open questions
- Support only Ollama and llama.cpp first, or MLX as well?
- Final name and domain check.
