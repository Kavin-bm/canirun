# canirun: Goal

## One-line pitch
One command that tells you whether an LLM will run on your machine, and how fast. Then it measures.

## The problem
People pick a model, download 20 GB, and find it does not fit in memory or runs at 2 tokens per second. The numbers live in scattered blog posts, spreadsheets and web calculators. A website cannot see your real hardware or measure real speed.

## Who it is for
Anyone running local models: developers, hobbyists, teams choosing hardware.

## What success looks like
1. `canirun llama-3.1-8b` prints a clear verdict (runs great, runs slowly, will not fit) with the quantization to use.
2. `canirun bench` measures real tokens per second on the installed runtime, and compares with the estimate.
3. `canirun recommend` lists the best models for this machine.
4. A shareable result card (text and image) people post on X, which helps the repo spread.
5. Works with zero setup: one static Go binary.

## Principles
- Simple, honest and fast to try: `brew install` or `curl | sh`, then one command.
- Estimates are labeled as estimates. Measured results are labeled as measured.
- The model catalog is data (`models/*.yaml`), so the community can add models by PR.
- Local only: no telemetry, no account. Sharing community benchmarks is opt-in.

## Non-goals
- Not an inference engine and not a model downloader. It reads from Hugging Face metadata and talks to existing runtimes (Ollama, llama.cpp).
- No training or fine-tuning estimates in v1.
