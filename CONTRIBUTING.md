# Contributing to canirun

Thanks for helping! The two most useful contributions take a few minutes and no Go knowledge.

## 1. Share a measured result (2 minutes)

```sh
canirun bench <ollama-model> --save      # e.g. canirun bench llama3.2 --save
canirun validate *.yaml
```

Copy the generated file into `models/results/` in your fork and open a PR titled `result: <model> on <chip>`. Your number then shows up as MEASURED for everyone with the same chip, and helps calibrate the estimate.

## 2. Add a model (2 minutes)

1. Copy an existing file in `models/`, e.g. `models/qwen3-8b.yaml`, to `models/<id>.yaml`.
2. Fill in from the model's `config.json` on Hugging Face:
   - `params_b`: total parameters in billions (the safetensors total on the model page)
   - `active_params_b`: mixture-of-experts models only, parameters used per token
   - `layers`: `num_hidden_layers`
   - `kv_heads`: `num_key_value_heads`
   - `head_dim`: `head_dim`, or `hidden_size / num_attention_heads`
   - `context_max`: `max_position_embeddings`
   - Multimodal configs keep these under `text_config`.
3. Add `aliases` people actually type (Ollama tags like `qwen3:8b`). Aliases must be unique.
4. Run `canirun validate models/<id>.yaml` (or `go run ./cmd/canirun validate models/<id>.yaml`) and open a PR titled `model: <name>`.

The format is flat `key: value` YAML (schema in `models/schema.json`) so canirun stays dependency-free.

## 3. Add your GPU's memory bandwidth

If `canirun hw` says bandwidth is ASSUMED, add your GPU to the table in `internal/hw/bandwidth.go` with the vendor's spec-sheet number and a test case in `hw_test.go`.

## Code

- Go 1.22+. Run `make lint test` before a PR (vet, staticcheck, gofmt, race tests).
- Keep the estimate math in `internal/estimate` with a unit test for every formula change.
- Shared answers live in `internal/report`, so the CLI, UI and MCP server never disagree.
- Never print a number as MEASURED unless it was measured. No telemetry, no new network calls, no new dependencies.
- See [GOOD_FIRST_ISSUES.md](GOOD_FIRST_ISSUES.md) for starter tasks.
