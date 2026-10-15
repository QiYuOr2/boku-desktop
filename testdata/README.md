# testdata

Vectors for the tests in this module: the raw responses the adapters map.

They mirror the repository-level `fixtures/providers/` directory, which both
ends share. **Change both copies.**

```text
fixtures/providers/<providerId>/<case>.json  ←→  testdata/providers/<providerId>/<case>.json
```

The copy exists because this module is published on its own and cannot reach
outside its own directory. The repository-level `fixtures/` stays the source of
truth for the Apple end; `fixtures/vault/` has no counterpart here because no
test in this module reads it yet (the encrypted vault is Task 008).
