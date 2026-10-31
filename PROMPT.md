# Paste this into your coding agent

Copy everything in the box into Claude Code, Codex, Gemini CLI, Cursor or any agent that can run shell commands. It checks which local LLMs your computer can run, explains the answer in plain words, and asks before doing anything that takes time.

```text
Help me find out which local AI models (LLMs) my computer can run, using the
open-source tool canirun (https://github.com/Kavin-bm/canirun). I am not
technical, so explain everything in plain, friendly words.

1. Check whether `canirun` is installed (`canirun version`). If it is not, tell
   me the install command for my system and ask me before running it:
   - macOS/Linux:  curl -fsSL https://raw.githubusercontent.com/Kavin-bm/canirun/main/install.sh | sh
   - With Go:      go install github.com/Kavin-bm/canirun/cmd/canirun@latest
2. Run `canirun hw --json` and describe my computer in one or two sentences
   (chip, memory, whether it has a graphics card that helps).
3. Run `canirun recommend --json` and tell me the 3-5 best models for me.
   For each one say: what it is good at (chat, coding, reasoning), how much
   memory it needs, and the estimated speed in words:
   30+ tokens/sec = "faster than you can read", 10-30 = "comfortable",
   4-10 = "usable but slow", under 4 = "painfully slow".
4. If I ask about a specific model, run `canirun <model> --json` and explain
   the verdict: "fits" (runs fully on the graphics chip, fast), "partial"
   (some of it runs on the CPU, slower) or "no" (not enough memory).
   Exit code 3 means it will not fit.
5. Be honest about labels: numbers marked ESTIMATE are calculations; only
   MEASURED numbers come from a real test. Say which is which.
6. Offer to measure real speed with `canirun bench <model>` only for models
   already installed in Ollama. Tell me it keeps the computer busy for up to
   a minute and ASK FIRST. Never download a model (`ollama pull`) without my
   explicit yes, and tell me how big the download is before asking.
7. If I want a visual page, run `canirun ui` (it opens a local page on
   127.0.0.1; nothing goes on the internet).
8. Do not change any system settings. canirun only reads hardware info.
```

## Prefer tools over shell commands?

canirun is also an MCP server (stdio), so agents can call it directly. The server is tested over stdio; the registration commands below follow each CLI's documented syntax but have not been run here:

```sh
claude mcp add canirun -- canirun mcp            # Claude Code
codex mcp add canirun -- canirun mcp             # Codex CLI
gemini mcp add canirun canirun mcp               # Gemini CLI
```

Tools: `detect_hardware`, `estimate_model`, `recommend_models`, `list_models`, `list_installed_models`, `benchmark_model` (asks the agent to confirm with you first).
