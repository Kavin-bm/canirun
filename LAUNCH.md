# Launch posts (drafts)

Replace the bracketed bits, attach the images from `docs/screenshots/`, and post once the
first release (binaries + `brew` + `curl | sh`) is live.

---

## X / Twitter thread

**1/**
[YOUR STORY, 1-2 lines, true details only. Shape: "I downloaded a <size> GB model, it
<didn't load / crawled at N tok/s> on my <machine>."]

The answer was knowable before I hit download. So I built a tool that tells you in one second.

`canirun qwen3:14b` 👇
[image: docs/screenshots/ui-wont-fit.png — "Llama 3.3 70B: won't fit"]

**2/**
It reads your real hardware (Apple Silicon, NVIDIA, AMD, macOS/Linux/Windows), does the memory math (weights + KV cache + overhead) for every quantization, and estimates speed from memory bandwidth.

Q5_K_M ✅ fits · 12 GB · ~17 tok/s. Done.

**3/**
Estimates lie, so it also measures.

`canirun bench llama3.2` runs your local Ollama and puts the real number next to the guess.
On my M4 Pro: estimate 93 tok/s, measured 89. 
[image: docs/screenshots/ui-after-bench.png]

**4/**
Every number is labelled ESTIMATE or MEASURED. It never downloads a model, never phones home, and the web UI only listens on 127.0.0.1.

One Go binary, zero dependencies.

**5/**
Built for agents too: `--json` everywhere, stable exit codes, and an MCP server.

`canirun qwen3:32b --json && ollama pull qwen3:32b` only downloads if it fits.

**6/**
50 models today. The catalog and benchmark results are plain YAML, so if you run `canirun bench --save` and open a PR, your GPU's real numbers help everyone.

⭐ github.com/kavinbm/canirun
[image: docs/screenshots/share-card.png]

---

## Show HN

**Title:** Show HN: canirun – will this LLM run on my machine, and how fast? (one Go binary)

**Body:**

Hi HN, [one true sentence about your own experience, e.g. which model was too big or too
slow for your laptop]. The numbers exist, but they're scattered across blog posts, spreadsheets and web
calculators, and a website can't see your actual GPU or measure anything.

canirun is a single Go binary (stdlib only) that:

- detects your hardware (Apple Silicon unified memory and the macOS GPU wired limit, NVIDIA
  via nvidia-smi incl. multi-GPU, AMD via sysfs or the Windows driver registry),
- estimates memory per quantization (weights + KV cache + overhead) and gives a verdict:
  fits on GPU, partial offload, or won't fit,
- estimates generation speed from memory bandwidth (a spec-sheet table of ~90 chips) and
  active parameters, so MoE models like Qwen3-30B-A3B and gpt-oss come out right,
- measures real speed through Ollama or llama.cpp's llama-bench and shows it next to the
  estimate. On my M4 Pro, Llama 3.2 3B Q4_K_M: estimate 93 tok/s, measured 89–94.

There's also a local web UI (`canirun ui`, 127.0.0.1 only, embedded assets, strict CSP),
`--json` with stable exit codes for scripts, and an MCP server so coding agents can ask
"can I run this?" directly.

What it doesn't do: download or run models itself, estimate prompt-processing speed (it only
measures that), or model sliding-window KV caches yet. The GPU efficiency factor is
calibrated on Apple Silicon so far; CPU-only and NVIDIA numbers will get better as people
submit `canirun bench --save` results, which are plain YAML in the repo.

I'd love feedback on the estimate math, and benchmark results from hardware I don't have
(especially Windows + AMD and multi-GPU Linux).

https://github.com/kavinbm/canirun
