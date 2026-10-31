# canirun audit

Audit of the initial scaffold (commit `3ef1be6`), done on 2026-10-31.

Tools run: `go build ./...`, `go vet ./...`, `go test -race ./...` (all passed),
`staticcheck ./...` (no findings), `golangci-lint run` (12 `errcheck` findings on
ignored `fs.Parse`/`fmt.Fprint` returns).

> Scope note: the task brief also listed `fixmyrouter` checks (no brute force, only
> the default gateway, consent and snapshots). This repository is **canirun only**,
> so those items do not apply here. The equivalent safety review for canirun is in
> "Safety review" below.

Severity: **High** = gives users a wrong answer or can hang/harm; **Medium** = wrong in
some setups, or breaks scripts/agents; **Low** = polish, naming, dead code.

| # | Severity | Finding | Status |
|---|---|---|---|
| 1 | High | On any OS where RAM is not detected (Windows today, unusual Linux containers) `RAMBytes` is 0, so every model gets a confident **"❌ no"**. Wrong answer, no warning. | Fixed: estimate/recommend now fail with exit 1 and a clear message instead of a false "no". Windows detection added in phase 2. |
| 2 | High | External probes (`nvidia-smi`, `system_profiler`, `lspci`) ran with no timeout. A wedged NVIDIA driver makes `nvidia-smi` hang, so `canirun` hangs. | Fixed: every probe uses a 5 s `context` timeout. |
| 3 | Medium | Intel Macs: `spdisplays_vram_shared` (iGPU memory carved out of RAM) was treated as dedicated VRAM, giving a "discrete GPU" budget and bogus "~X% on GPU" verdicts. | Fixed: only `spdisplays_vram` counts; test added. |
| 4 | Medium | Multi-GPU NVIDIA boxes: only the first GPU's VRAM was counted, so 2x3090 (48 GB) was treated as 24 GB. | Fixed: VRAM is summed, name shows `2x ...`; test added. |
| 5 | Medium | Linux AMD: VRAM was read from the first `card*` in sysfs, often a small iGPU. | Fixed: largest card wins. |
| 6 | Medium | Exit codes were accidental (flag errors 2 via `os.Exit` inside `flag`, other errors 1, "won't fit" 0). Scripts could not tell "won't fit" from success. | Fixed: documented contract 0 ok, 1 error, 2 usage, 3 won't fit; tested. |
| 7 | Medium | With `--json`, errors were plain text on stderr, so agents could not parse failures. | Fixed: `{"error", "exit_code"}` JSON on stdout; tested. |
| 8 | Medium | `main` called `os.Exit` from deep inside flag parsing and wrote straight to `os.Stdout`, so the CLI had zero tests. | Fixed: `run(args, stdout, stderr) int` with table tests for every exit code. |
| 9 | Medium | Model YAML parser silently ignored unknown keys, so a typo (`kv_head`, `contxt_max`) in a community PR was dropped without error. | Fixed: unknown keys are an error; test added. |
| 10 | Medium | README quick start claimed `go run github.com/kavinbm16/canirun/...@latest` works, but the module path is a placeholder and the repo is not published. | Fixed in the phase 5 README (marked as available after the first release). |
| 11 | Low | `canirun 0b` built a 0-parameter model that "fits". | Fixed: sizes must be > 0 and <= 10000B. |
| 12 | Low | A partial-offload GPU share was rounded, so 99.6% printed as "~100% on GPU" next to a "partial" verdict. | Fixed: floor instead of round. |
| 13 | Low | `--ctx` above the model's `context_max` was accepted silently. | Fixed: warning in text and JSON. |
| 14 | Low | Ollama names with `:latest` (`llama3.1:latest`) did not resolve. | Fixed. |
| 15 | Low | `list` had no `--json`; `hw extra-arg` silently ignored the extra argument. | Fixed. |
| 16 | Low | Dead code: `append([]Model{}, cat...)` copy in a test; `run` in `.PHONY` with no target. | Fixed. |
| 17 | Low | `make test` ran without `-race`; `make lint` had no staticcheck. | Fixed. |
| 18 | Low | KV cache is overestimated for sliding-window models (Gemma 2/3, gpt-oss) and MLA models (DeepSeek V3/R1) unless the catalog encodes it. | Documented; MLA encoded in catalog entries (phase 2). Sliding window: coming soon. |
| 19 | Low | Apple Silicon budget ignores a user-raised `iogpu.wired_limit_mb`. | Fixed in phase 2. |
| 20 | Low | `golangci-lint` errcheck flags unchecked `fmt.Fprint*` to writers. | Won't fix: printing to stdout; CI runs vet + staticcheck. |

## Safety review (canirun)

- **Read-only.** Hardware detection only reads (`sysctl`, `system_profiler`, `/proc`, `/sys`,
  `nvidia-smi`, `lspci`, PowerShell CIM queries on Windows). No writes outside the current
  directory, and only when the user asks (`bench --save`).
- **No network by default.** The estimate path never touches the network. `bench` talks
  only to the local Ollama API (`127.0.0.1:11434` unless `OLLAMA_HOST` says otherwise),
  never pulls a model, and skips Ollama `:cloud` models, which would send prompts off the machine.
- **Local UI** (phase 3) binds `127.0.0.1` only and rejects requests whose `Host` or `Origin`
  is not local (DNS-rebinding and cross-site request protection), with a strict CSP.
- No telemetry, no accounts.
