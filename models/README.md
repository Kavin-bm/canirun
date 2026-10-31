# Model catalog and measured results

- `*.yaml`: one file per model (schema: [`schema.json`](schema.json)). Fields come from the model's `config.json` on Hugging Face.
- `results/*.yaml`: MEASURED benchmarks contributed by the community (schema: [`result.schema.json`](result.schema.json)), written by `canirun bench <model> --save`. `canirun <model>` shows them next to the estimate.

The format is a flat `key: value` subset of YAML so canirun stays dependency-free. Check your file with:

```sh
canirun validate models/my-model.yaml
canirun validate models/results/my-result.yaml
```

See [CONTRIBUTING.md](../CONTRIBUTING.md).
