# Contributing to canirun

## Add a model (easiest)

1. Copy an existing file in `models/`, e.g. `models/llama3.1-8b.yaml`, to `models/<id>.yaml`.
2. Fill in from the model's `config.json` on Hugging Face:
   - `params_b`: total parameters in billions
   - `layers`: `num_hidden_layers`
   - `kv_heads`: `num_key_value_heads`
   - `head_dim`: `head_dim`, or `hidden_size / num_attention_heads`
   - `context_max`: `max_position_embeddings`
3. Add `aliases` people actually type (e.g. Ollama tags like `qwen2.5:7b`). Aliases must be unique.
4. Run `make test` (it checks every catalog file loads) and open a PR titled `model: <name>`.

The format is a flat `key: value` subset of YAML so canirun stays dependency-free; no nesting.

## Submit measured results (coming soon)

`canirun bench` will write a result file you can PR into `models/results/`. Those numbers will calibrate the estimates.

## Code

- Go 1.22+. Run `make lint test` before a PR.
- Keep the estimate math in `internal/estimate` with a unit test for every formula change.
- Never print a number as measured unless it was measured.
