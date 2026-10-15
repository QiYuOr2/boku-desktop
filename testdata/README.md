# testdata

Sample responses for the adapter tests: the raw JSON each provider sends,
mapped offline into a `domain.Snapshot`.

```text
providers/<providerId>/<case>.json
```

Every provider needs at least a happy path and a malformed case. The malformed
one asserts that the adapter returns `ErrProviderBadResponse` rather than
reporting a failure as `0`.

These are the service's own structures, not rewritten for convenience — a
reshaped fixture hides the real field names and units. Amounts and durations
are converted inside the adapter, so the fixtures stay as they arrive.
