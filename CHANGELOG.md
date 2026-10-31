# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Speed estimate (tokens/sec) from memory bandwidth, mixture-of-experts aware, calibrated against measured runs.
- `canirun bench`: real measurements with Ollama or llama.cpp's `llama-bench`, compared with the estimate; `--save` writes a community result file.
- Community results in `models/results/`, shown as MEASURED next to estimates.
- `canirun ui`: local web UI (127.0.0.1 only) with verdicts, estimated vs measured chart, one-click benchmark, PNG result card.
- `canirun mcp`: MCP server for coding agents.
- `canirun validate`, JSON schemas for catalog and result files, issue and PR templates.
- What-if flags `--ram`, `--vram`, `--bw`.
- Windows hardware detection; multi-GPU NVIDIA; Apple `iogpu.wired_limit_mb`.
- `--json` on every command and stable exit codes (0 ok, 1 error, 2 usage, 3 won't fit, 4 runtime unavailable).
- Catalog expanded to 50 popular models.
- GoReleaser config, Homebrew cask, `install.sh` with checksum verification.

### Fixed
- Undetected RAM produced a false "won't fit" for every model; it is now an error.
- Hardware probes could hang forever; they now time out after 5 s.
- Intel Mac shared GPU memory, small iGPU carve-outs and the first AMD card were treated as discrete VRAM.
- See [AUDIT.md](AUDIT.md) for the full list.

## [0.1.0-dev] - initial scaffold
- Memory estimate, `hw`, `list`, `recommend` for macOS and Linux.
