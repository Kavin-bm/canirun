# Security policy

## Reporting a vulnerability

Please do **not** open a public issue. Use GitHub's private vulnerability reporting
("Security" tab → "Report a vulnerability") on this repository. You'll get a reply within
7 days. Please include the canirun version (`canirun version`), your OS, and steps to reproduce.

## What canirun does, so you can judge the attack surface

- **Hardware detection** runs read-only system tools with a 5 s timeout: `sysctl`,
  `system_profiler`, `nvidia-smi`, `lspci`, `lscpu`, and on Windows one PowerShell CIM query.
  No arguments come from user input.
- **`canirun bench`** talks only to the Ollama API at `OLLAMA_HOST` (default
  `127.0.0.1:11434`), or runs `llama-bench` on a file path you give it. It never pulls
  models and refuses Ollama cloud models.
- **`canirun ui`** listens on `127.0.0.1` only. Every request must have a loopback `Host`
  (blocks DNS rebinding); a present `Origin` must match the server (blocks cross-site
  requests); POSTs must be JSON. Responses carry a strict Content-Security-Policy and the
  page makes no external requests. Only one benchmark can run at a time.
- **`canirun mcp`** reads JSON-RPC from stdin and writes to stdout. No network listener.
- **`install.sh`** downloads from GitHub Releases over HTTPS and verifies SHA-256 against
  `checksums.txt` before installing. It never uses `sudo`.
- No telemetry, no accounts, no auto-update.

## Supported versions

Only the latest release gets fixes.
